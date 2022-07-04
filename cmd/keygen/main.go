package main

import (
	"cs.ubc.ca/cpsc416/BlockVote/Identity"
	"fmt"
)

func main() {
	signer := Identity.CreateSigner("anom", "anom")
	fmt.Println("private key: ", signer.Export())
	fmt.Println(fmt.Sprintf("public key: %x", signer.PublicKey))
}
