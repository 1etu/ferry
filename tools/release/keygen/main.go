package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
)

const bytesPerRow = 8

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(out io.Writer) error {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}
	seed := base64.StdEncoding.EncodeToString(private.Seed())
	_, err = fmt.Fprintf(out, "FERRY_SIGNING_KEY=%s\n\n%s", seed, goLiteral(public))
	return err
}

func goLiteral(public ed25519.PublicKey) string {
	var b strings.Builder
	b.WriteString("var publicKey = ed25519.PublicKey{\n")
	for row := 0; row < len(public); row += bytesPerRow {
		b.WriteString("\t")
		for i, c := range public[row : row+bytesPerRow] {
			if i > 0 {
				b.WriteString(" ")
			}
			fmt.Fprintf(&b, "0x%02x,", c)
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return b.String()
}
