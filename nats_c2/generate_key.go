// generate_key.go
package main

import (
    "crypto/rand"
    "encoding/base64"
    "fmt"
)

func main() {
    key := make([]byte, 32)
    rand.Read(key)
    fmt.Println(base64.StdEncoding.EncodeToString(key))
}
