package nodeagent

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// packagedBinarySuffix is the client's path inside its .deb and .rpm,
// matched as a suffix because the two formats disagree on the leading
// "./" and "/".
const packagedBinarySuffix = "usr/bin/node-agent-client"

// maxExtractedBinary bounds how much is written when unpacking, so a
// crafted archive cannot fill the disk. Far above any real client.
const maxExtractedBinary = 512 << 20

// extractPackagedBinary writes the client binary found inside the .deb
// or .rpm at pkgPath to dst. Both formats are read here rather than by
// dpkg-deb or rpm2cpio so the check before an install does not depend
// on which tools a node has; the packages are built with gzip payloads
// for exactly this (see cmd/build-agent-packages).
func extractPackagedBinary(format, pkgPath, dst string) error {
	f, err := os.Open(pkgPath)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReader(f)

	var payload io.Reader
	switch format {
	case installMethodDeb:
		payload, err = debDataTar(r)
	case installMethodRPM:
		payload, err = rpmPayload(r)
	default:
		return fmt.Errorf("cannot unpack a %q artifact", format)
	}
	if err != nil {
		return fmt.Errorf("read %s package: %w", format, err)
	}

	gz, err := gzip.NewReader(payload)
	if err != nil {
		return fmt.Errorf("read %s package payload: %w", format, err)
	}
	defer gz.Close()

	var body io.Reader
	if format == installMethodDeb {
		body, err = findInTar(gz)
	} else {
		body, err = findInCpio(gz)
	}
	if err != nil {
		return fmt.Errorf("read %s package contents: %w", format, err)
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(body, maxExtractedBinary+1))
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err == nil && n > maxExtractedBinary {
		err = errors.New("the packaged client is implausibly large")
	}
	if err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}

// debDataTar positions r at the data.tar.gz member of a .deb, which is
// an ar archive: an 8-byte magic, then members each led by a 60-byte
// header and padded to an even length.
func debDataTar(r *bufio.Reader) (io.Reader, error) {
	magic := make([]byte, 8)
	if _, err := io.ReadFull(r, magic); err != nil || string(magic) != "!<arch>\n" {
		return nil, errors.New("not an ar archive")
	}
	for {
		header := make([]byte, 60)
		if _, err := io.ReadFull(r, header); err != nil {
			return nil, errors.New("no data.tar.gz member found")
		}
		name := strings.TrimRight(strings.TrimSpace(string(header[:16])), "/")
		size, err := strconv.ParseInt(strings.TrimSpace(string(header[48:58])), 10, 64)
		if err != nil || size < 0 {
			return nil, errors.New("malformed ar member header")
		}
		if name == "data.tar.gz" {
			return io.LimitReader(r, size), nil
		}
		if strings.HasPrefix(name, "data.tar") {
			return nil, fmt.Errorf("unsupported payload compression %q", name)
		}
		if _, err := io.CopyN(io.Discard, r, size+size%2); err != nil {
			return nil, errors.New("truncated ar archive")
		}
	}
}

func findInTar(r io.Reader) (io.Reader, error) {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err != nil {
			return nil, errors.New("the client binary is not in the package")
		}
		if h.Typeflag == tar.TypeReg && strings.HasSuffix(h.Name, packagedBinarySuffix) {
			return tr, nil
		}
	}
}

// rpmPayload positions r at an .rpm's payload: past the 96-byte lead,
// the signature header (padded to 8 bytes) and the main header.
func rpmPayload(r *bufio.Reader) (io.Reader, error) {
	lead := make([]byte, 96)
	if _, err := io.ReadFull(r, lead); err != nil || !bytes.Equal(lead[:4], []byte{0xed, 0xab, 0xee, 0xdb}) {
		return nil, errors.New("not an rpm package")
	}
	for i := range 2 {
		intro := make([]byte, 16)
		if _, err := io.ReadFull(r, intro); err != nil || !bytes.Equal(intro[:3], []byte{0x8e, 0xad, 0xe8}) {
			return nil, errors.New("malformed rpm header")
		}
		entries := int64(binary.BigEndian.Uint32(intro[8:12]))
		store := int64(binary.BigEndian.Uint32(intro[12:16]))
		length := entries*16 + store
		if i == 0 && length%8 != 0 {
			length += 8 - length%8
		}
		if _, err := io.CopyN(io.Discard, r, length); err != nil {
			return nil, errors.New("truncated rpm header")
		}
	}
	return r, nil
}

// findInCpio reads a "new ASCII" cpio archive, which is what an rpm
// payload is: a 110-byte header of 8-digit hex fields, the file name,
// then the data, each padded to 4 bytes.
func findInCpio(r io.Reader) (io.Reader, error) {
	notFound := errors.New("the client binary is not in the package")
	for {
		header := make([]byte, 110)
		if _, err := io.ReadFull(r, header); err != nil {
			return nil, notFound
		}
		if magic := string(header[:6]); magic != "070701" && magic != "070702" {
			return nil, errors.New("unsupported cpio format")
		}
		field := func(i int) (int64, error) {
			return strconv.ParseInt(string(header[6+i*8:14+i*8]), 16, 64)
		}
		fileSize, err1 := field(6)
		nameSize, err2 := field(11)
		if err1 != nil || err2 != nil || nameSize <= 0 || nameSize > 4096 {
			return nil, errors.New("malformed cpio header")
		}
		name := make([]byte, nameSize)
		if _, err := io.ReadFull(r, name); err != nil {
			return nil, notFound
		}
		if pad := (110 + nameSize) % 4; pad != 0 {
			if _, err := io.CopyN(io.Discard, r, 4-pad); err != nil {
				return nil, notFound
			}
		}
		fileName := strings.TrimRight(string(name), "\x00")
		if fileName == "TRAILER!!!" {
			return nil, notFound
		}
		if strings.HasSuffix(fileName, packagedBinarySuffix) && fileSize > 0 {
			return io.LimitReader(r, fileSize), nil
		}
		skip := fileSize
		if pad := fileSize % 4; pad != 0 {
			skip += 4 - pad
		}
		if _, err := io.CopyN(io.Discard, r, skip); err != nil {
			return nil, notFound
		}
	}
}
