// Command aipet-feedsign is an admin tool for the enterprise feed operator. It
// generates an ed25519 keypair and signs a manifest so client binaries can verify
// authenticity. The private key never ships in the product; only the public key
// is distributed to clients (via `aipet config feed_public_key`).
//
// usage:
//
//	aipet-feedsign keygen                       # print a new keypair (base64)
//	aipet-feedsign sign <priv-b64> <manifest>   # emit a signed envelope to stdout
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "keygen":
		pub, priv, err := ed25519.GenerateKey(nil)
		must(err)
		fmt.Printf("public_key:  %s\n", base64.StdEncoding.EncodeToString(pub))
		fmt.Printf("private_key: %s\n", base64.StdEncoding.EncodeToString(priv))
		fmt.Fprintln(os.Stderr, "\nDistribute the public key to clients; keep the private key secret.")
	case "sign":
		if len(os.Args) != 4 {
			usage()
			os.Exit(2)
		}
		priv, err := base64.StdEncoding.DecodeString(os.Args[2])
		must(err)
		if len(priv) != ed25519.PrivateKeySize {
			fatal("invalid private key size")
		}
		manifest, err := os.ReadFile(os.Args[3])
		must(err)
		// Validate it parses as a manifest before signing.
		var probe map[string]any
		must(json.Unmarshal(manifest, &probe))

		// Sign the exact manifest bytes and carry them base64-encoded so JSON
		// re-formatting of the envelope can never alter what gets verified.
		sig := ed25519.Sign(ed25519.PrivateKey(priv), manifest)
		env := map[string]any{
			"manifest_b64": base64.StdEncoding.EncodeToString(manifest),
			"signature":    base64.StdEncoding.EncodeToString(sig),
		}
		out, err := json.MarshalIndent(env, "", "  ")
		must(err)
		fmt.Println(string(out))
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `aipet-feedsign — enterprise feed signing tool

usage:
  aipet-feedsign keygen
  aipet-feedsign sign <private-key-base64> <manifest.json>
`)
}

func must(err error) {
	if err != nil {
		fatal(err.Error())
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "aipet-feedsign:", msg)
	os.Exit(1)
}
