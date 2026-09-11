package agentdist

import (
	"strings"
	"text/template"
)

// openvoxAgentWindowsVersion is hardcoded, not resolved dynamically:
// unlike apt/yum (repo metadata always resolves "install openvox-agent"
// to whatever's current), OpenVox publishes individual versioned MSIs
// with no "latest" alias - confirmed against downloads.voxpupuli.org's
// real directory listing, and against OpenVox's own
// puppet-openvox_bootstrap module (tasks/install_windows.ps1), whose own
// version-resolution logic hardcodes a version with a "XXX: Move this
// metadata out to the openvox-agent build pipeline" comment
// acknowledging the same gap. Bump this when a newer release is
// confirmed available - there is no dynamic way to do this today, on
// either side.
const openvoxAgentWindowsVersion = "8.29.0"

// installScriptWindowsTemplate is rendered by installScriptWindows
// (handlers.go) with Config as its data - the Windows counterpart to
// installScriptTemplate. See design.md in add-multi-platform-agent-install
// for why Windows needs its own script (PowerShell, MSI installer,
// Service Control Manager) rather than branching install.sh further.
// Deliberately avoids PowerShell's backtick escape character throughout
// (array-form -ArgumentList, string concatenation for embedded quotes)
// since a literal backtick can't appear inside this Go raw string.
const installScriptWindowsTemplate = `#Requires -RunAsAdministrator
$ErrorActionPreference = "Stop"

$ConsoleUrl = {{psQuote .ConsoleBaseURL}}
$TransportAddr = {{psQuote .TransportAddr}}
# Empty when the console has no node-facing Puppet server address
# configured - enrollment below then leaves this node's own server
# setting alone (see design.md in add-install-script-auto-enrollment).
$PuppetServer = {{psQuote .PuppetServerAddr}}
# waitforcert is the poll interval, maxwaitforcert the total wait before
# giving up - setting only the former polls forever (see design.md).
$CertPollSeconds = 15
$CertMaxWaitSeconds = 300

if (-not ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Error "This script must be run as Administrator (it installs a package and a Windows service)."
    exit 1
}

$PuppetBin = "C:\Program Files\Puppet Labs\Puppet\bin\puppet.exe"

if (-not (Test-Path $PuppetBin)) {
    Write-Host "Installing openvoxagent..."
    $OpenvoxAgentVersion = {{psQuote openvoxAgentVersion}}
    $MsiUrl = "https://downloads.voxpupuli.org/windows/openvox8/openvox-agent-$OpenvoxAgentVersion-x64.msi"
    $MsiPath = Join-Path $env:TEMP "openvox-agent-$OpenvoxAgentVersion-x64.msi"
    $InstallLog = Join-Path $env:TEMP "openvox-agent-install.log"
    Invoke-WebRequest -Uri $MsiUrl -OutFile $MsiPath
    Start-Process -FilePath "msiexec.exe" -ArgumentList @("/i", $MsiPath, "/qn", "/norestart", "/log", $InstallLog) -Wait
    Remove-Item -Path $MsiPath -ErrorAction SilentlyContinue
} else {
    Write-Host "openvoxagent already installed, skipping."
}

Write-Host "Setting up package-inventory reporting..."
$FactsDDir = "C:\ProgramData\PuppetLabs\facter\facts.d"
New-Item -ItemType Directory -Force -Path $FactsDDir | Out-Null
# External fact (see design.md in add-package-inventory-reporting):
# recomputed on every Facter run, not just once at install time. A
# literal here-string ('@ ... @') so none of this gets expanded by
# *this* script - it's written out verbatim for Facter to run later.
$PackageInventoryFact = @'
$Packages = @(Get-Package | ForEach-Object { , @($_.Name, [string]$_.Version, $_.ProviderName) })
$Result = @{ "_puppet_inventory_1" = @{ "packages" = $Packages } }
$Result | ConvertTo-Json -Depth 4 -Compress
'@
Set-Content -Path (Join-Path $FactsDDir "package_inventory.ps1") -Value $PackageInventoryFact -Encoding ASCII

$Certname = (& $PuppetBin config print certname).Trim()
$SslDir = (& $PuppetBin config print ssldir).Trim()
$CertFile = Join-Path $SslDir "certs\$Certname.pem"
$KeyFile = Join-Path $SslDir "private_keys\$Certname.pem"
$CaFile = Join-Path $SslDir "certs\ca.pem"

if (-not (Test-Path $CertFile)) {
    # Same enrollment flow as the Linux script - see design.md in
    # add-install-script-auto-enrollment. Static-verified only: this
    # project's dev environment has no Windows host to run it on.
    if ($PuppetServer -ne "") {
        Write-Host "Pointing this node at Puppet server $PuppetServer..."
        & $PuppetBin config set server $PuppetServer
    }

    $EnrollServer = (& $PuppetBin config print server).Trim()
    Write-Host "Enrolling $Certname with the Puppet CA at $EnrollServer (waiting up to ${CertMaxWaitSeconds}s for the certificate to be signed)..."

    # 'ssl bootstrap', not 'agent -t': it requests and downloads a
    # certificate and nothing else, so a catalog failure can't be
    # misreported as an enrollment failure. Its own waitforcert polling
    # is the wait. $ErrorActionPreference is relaxed for this one call
    # because a CA that is unreachable, or a request still waiting to be
    # signed, both make it exit non-zero - which branch we are in is
    # decided below by whether the certificate now exists, not by the
    # exit code (see design.md).
    $PreviousErrorAction = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    $EnrollOutput = (& $PuppetBin ssl bootstrap --waitforcert $CertPollSeconds --maxwaitforcert $CertMaxWaitSeconds 2>&1 | Out-String)
    $ErrorActionPreference = $PreviousErrorAction
    Write-Host $EnrollOutput

    if (-not (Test-Path $CertFile)) {
        if ($EnrollOutput -match 'No more routes to ca|Failed to open TCP connection|certificate verify failed|Connection refused') {
            Write-Error ("Could not reach the Puppet CA at $EnrollServer - see the error above. " +
                "This is a connectivity or TLS problem, not a certificate waiting to be signed. " +
                "Check that $EnrollServer resolves from this node, that port 8140 is reachable, " +
                "and that the name matches the CA certificate's SANs.")
            exit 1
        }

        Write-Error ("This node's certificate request was submitted but has not been signed yet. " +
            "Sign it in the console (Nodes page): $ConsoleUrl/nodes.html " +
            "Then re-run this script - it will pick up from here.")
        exit 1
    }

    Write-Host "Certificate signed and installed at $CertFile."
}

$InstallDir = "C:\Program Files\OpenVox Console"
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$BinaryPath = Join-Path $InstallDir "node-agent-client.exe"

# Stopped before downloading, not after: unlike Linux/macOS, Windows
# locks a running executable's file against being overwritten, so on a
# re-run this download would fail outright while the service is active.
$ServiceName = "node-agent-client"
$ExistingService = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($ExistingService -and $ExistingService.Status -eq 'Running') {
    Write-Host "Stopping existing node-agent-client service to update its binary..."
    Stop-Service -Name $ServiceName
}

Write-Host "Downloading node-agent-client (windows/amd64)..."
Invoke-WebRequest -Uri "$ConsoleUrl/packages/node-agent-client?os=windows&arch=amd64" -OutFile $BinaryPath

# Persisted as Machine-scope environment variables so the service
# (run by the SCM, not this interactive session) can read them via the
# same os.Getenv-based config loading node-agent-client uses on every
# platform - see internal/nodeagent.LoadConfig. NOTE: a Machine-scope
# environment variable change is not guaranteed to be visible to a
# freshly-started service until the next reboot on some Windows
# versions (services.exe's own environment block is cached at boot) -
# this could not be verified live in this project's Linux-only dev
# environment (see design.md's Risks). If the service starts but fails
# to connect, a reboot (or re-running this script, which restarts the
# service) is the standard workaround.
[Environment]::SetEnvironmentVariable("NODE_AGENT_TRANSPORT_ADDR", $TransportAddr, "Machine")
[Environment]::SetEnvironmentVariable("NODE_AGENT_CERT_FILE", $CertFile, "Machine")
[Environment]::SetEnvironmentVariable("NODE_AGENT_KEY_FILE", $KeyFile, "Machine")
[Environment]::SetEnvironmentVariable("NODE_AGENT_CA_FILE", $CaFile, "Machine")
[Environment]::SetEnvironmentVariable("NODE_AGENT_PUPPET_BIN_PATH", $PuppetBin, "Machine")

# A service's binary path must be quoted in the registry if it contains
# spaces (as this one does), or Windows misparses it at service-start
# time - built via concatenation rather than an embedded escape so this
# works the same regardless of PowerShell's own quoting rules.
$QuotedBinaryPath = '"' + $BinaryPath + '"'

if ($ExistingService) {
    Write-Host "node-agent-client service already registered, starting it with the updated binary..."
    Start-Service -Name $ServiceName
} else {
    New-Service -Name $ServiceName -BinaryPathName $QuotedBinaryPath -DisplayName "OpenVox Console node-agent-client" -StartupType Automatic
    Start-Service -Name $ServiceName
}

Write-Host "node-agent-client installed and started."
`

// psQuote renders s as a single-quoted PowerShell string literal,
// doubling embedded single quotes (PowerShell's own escape rule for
// single-quoted strings) - the PowerShell counterpart to pql.go's
// pqlString and install_script.go's bash %q quoting.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

var installTmplWindows = template.Must(template.New("install.ps1").Funcs(template.FuncMap{
	"psQuote":             psQuote,
	"openvoxAgentVersion": func() string { return openvoxAgentWindowsVersion },
}).Parse(installScriptWindowsTemplate))
