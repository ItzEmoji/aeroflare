package narinfo

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// Narinfo represents the metadata file for a NAR archive in a binary cache.
// See: https://nixos.org/manual/nix/stable/protocols/binary-cache-protocol.html
type Narinfo struct {
	StorePath   string
	URL         string
	Compression string
	FileHash    string // sha256:<base32>
	FileSize    int64
	NarHash     string // sha256:<base32>
	NarSize     int64
	References  []string // hash-name pairs, e.g. "abc123-hello-2.10"
	Deriver     string   // hash-name.drv or empty
	System      string
	Sig         string // key-name:base64-signature
}

// String serializes the Narinfo to the narinfo text format.
func (n *Narinfo) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "StorePath: %s\n", n.StorePath)
	fmt.Fprintf(&b, "URL: %s\n", n.URL)
	fmt.Fprintf(&b, "Compression: %s\n", n.Compression)
	fmt.Fprintf(&b, "FileHash: %s\n", n.FileHash)
	fmt.Fprintf(&b, "FileSize: %d\n", n.FileSize)
	fmt.Fprintf(&b, "NarHash: %s\n", n.NarHash)
	fmt.Fprintf(&b, "NarSize: %d\n", n.NarSize)
	if len(n.References) > 0 {
		fmt.Fprintf(&b, "References: %s\n", strings.Join(n.References, " "))
	} else {
		b.WriteString("References:\n")
	}
	if n.Deriver != "" {
		fmt.Fprintf(&b, "Deriver: %s\n", n.Deriver)
	} else {
		b.WriteString("Deriver:\n")
	}
	if n.System != "" {
		fmt.Fprintf(&b, "System: %s\n", n.System)
	}
	if n.Sig != "" {
		fmt.Fprintf(&b, "Sig: %s\n", n.Sig)
	}
	return b.String()
}

// Parse deserializes a narinfo text into a Narinfo struct.
func Parse(data string) (*Narinfo, error) {
	n := &Narinfo{}
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		idx := strings.Index(line, ": ")
		if idx < 0 {
			continue
		}
		key := line[:idx]
		value := line[idx+2:]

		switch key {
		case "StorePath":
			n.StorePath = value
		case "URL":
			n.URL = value
		case "Compression":
			n.Compression = value
		case "FileHash":
			n.FileHash = value
		case "FileSize":
			n.FileSize, _ = strconv.ParseInt(value, 10, 64)
		case "NarHash":
			n.NarHash = value
		case "NarSize":
			n.NarSize, _ = strconv.ParseInt(value, 10, 64)
		case "References":
			if value != "" {
				n.References = strings.Fields(value)
			}
		case "Deriver":
			if value != "" {
				n.Deriver = value
			}
		case "System":
			n.System = value
		case "Sig":
			n.Sig = value
		}
	}
	return n, scanner.Err()
}
