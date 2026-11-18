param([string]$name)
Write-Output (@{ greeting = "hello $name" } | ConvertTo-Json -Compress)
