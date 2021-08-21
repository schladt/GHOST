// Package client - GHOST Client package
// Mike Schladt - 2021
package client

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"ghost/agent/comms"
	"ghost/agent/logger"
	"io"
	"io/ioutil"
	mathrand "math/rand"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/matishsiao/goInfo"
)

// Client struct stores information about the local system
type Client struct {
	UUID         string
	InstallDir   string
	InstallName  string
	Initialized  bool
	Hostname     string
	ConfigPath   string
	ConfigHash   string
	BinaryHash   string
	Debug        bool
	Offline      bool
	Domain       string
	FQDN         string
	Architecture string
	OSVersion    string
	PublicKey    string
	PrivateKey   string
	CertName     string
	LocalDbName  string
	Version      string
	Interfaces   []map[string]string
	LocalPort    uint64
	PollTime     time.Duration
	Config       Config
	Log          logger.Logger
	Sender       comms.Sender
	LocalDb      Database
	PluginLock   sync.Mutex
}

// Config struct to hold configuration data
type Config struct {
	BinaryHash        string   `yaml:"BinaryHash"`
	Tags              string   `yaml:"Tags"`
	LogLevel          string   `yaml:"LogLevel"`
	ControllerList    []string `yaml:"ControllerList"`
	ProxyList         []string `yaml:"ProxyList"`
	ProxyBlackList    []string `yaml:"ProxyBlackList"`
	UseSystemProxies  bool     `yaml:"UseSystemProxies"`
	PollTime          int      `yaml:"PollTime"`
	ServerCertificate string   `yaml:"ServerCertificate"`
	Plugins           []Plugin `yaml:"Plugins"`
}

// Bootstrap builds client object and initializes if needed
func (client *Client) Bootstrap() {
	// take hash of binary and configuration file
	var err error
	client.BinaryHash, err = client.GetSHA256(os.Args[0])
	if err != nil {
		client.Log.Fatal("Could not get hash of current binary: %v", err)
	}

	client.ConfigHash, err = client.GetSHA256(client.ConfigPath)
	if err != nil {
		client.Log.Fatal("Could not get hash of current configuration file: %v", err)
	}

	// create local database
	client.LocalDbName = filepath.Join(client.InstallDir, "ghost.db") //TODO: Make generic
	client.LocalDb = Database{Name: client.LocalDbName}
	client.LocalDb.Init()

	// update log level
	client.Log.Level = client.Config.LogLevel
	client.Log.Info("Agent starting...")

	// set polltime
	mathrand.Seed(time.Now().UnixNano())
	client.PollTime = (time.Second * time.Duration(client.Config.PollTime)) + (time.Millisecond * time.Duration(mathrand.Intn(1000)))

	// check if client previously initialized
	isInitialized, err := client.LocalDb.KeyStoreSelect("IsInitialized")
	if err != nil {
		client.Log.Fatal("Unable to read from key store: %v", err)
	}

	if isInitialized == "true" {
		client.Initialized = true
	} else {
		client.Initialized = false
	}

	// Initialize client if needed
	if !client.Initialized {
		client.Log.Info("Client has not been initialized, initializing now...")
		client.Initialize()
	}

	// Read values from database
	err = client.KeyStoreReadIn()
	if err != nil {
		client.Log.Fatal(err.Error())
	}

	// Return now if offline
	if client.Offline {
		return
	}

	// find proxies from system if needed
	var foundProxies []string
	if client.Config.UseSystemProxies {
		foundProxies, err = comms.FindProxies()
		if err != nil {
			client.Log.Error("Error finding system proxies: %v", err)
		}
	}

	// deduplicate and remove black listed proxies
	for _, proxy := range foundProxies {
		addProxy := true
		for _, stopWord := range append(client.Config.ProxyList, client.Config.ProxyBlackList...) {
			if strings.Contains(strings.ToLower(proxy), strings.ToLower(stopWord)) {
				addProxy = false
			}
		}
		if addProxy {
			client.Config.ProxyList = append(client.Config.ProxyList, proxy)
		}
	}

	// initalize url and proxy to first in list
	var proxy string
	if client.Config.ProxyList != nil {
		proxy = client.Config.ProxyList[0]
	}
	controllerURL := client.Config.ControllerList[0]

	//create and initialize comm sender
	client.Sender = comms.Sender{
		ControllerURL:     controllerURL,
		Proxy:             proxy,
		ClientUUID:        client.UUID,
		ClientPrivateKey:  client.PrivateKey,
		ClientPublicKey:   client.PublicKey,
		Log:               &client.Log,
		ServerCertificate: client.Config.ServerCertificate,
	}

	err = client.Sender.Init()
	if err != nil {
		client.Log.Fatal("Could not create communication sender: %v", err)
	}

	// check if client is registered
	if client.UUID == "" {

		// loop until registration is successful
		for client.UUID == "" {
			client.Log.Info("Client not registered with controller. Beginning registration process...")

			messageMap := make(map[string]string)
			hash, err := client.GetSHA256(os.Args[0])
			if err != nil {
				client.Log.Fatal("Could not get hash of current binary: %v", err)
			}
			messageMap["hash"] = hash
			messageMap["hostname"] = client.Hostname
			messageMap["os_version"] = client.OSVersion
			messageMap["domain"] = client.Domain
			messageMap["fqdn"] = client.FQDN
			messageMap["architecture"] = client.Architecture
			interfaces, _ := json.Marshal(client.Interfaces)
			messageMap["interfaces"] = string(interfaces)
			messageMap["public_key"] = client.PublicKey
			messageMap["tags"] = client.Config.Tags

			jsonStr, err := json.Marshal(&messageMap)
			if err != nil {
				client.Log.Fatal("Unable to serialize registration message: %v", err)
			}

			resp, err := client.Sender.Send(jsonStr, "/core/register/")
			if err != nil {
				client.Log.Error("Error sending registration message: %s", err)
				//attempt different controller & proxy combinations
				client.Sender.UpdateConnection(client.Config.ProxyList, client.Config.ControllerList)
				time.Sleep(time.Second * 2)
			} else {
				// parse json response and save uuid
				respMap := make(map[string]string)
				json.Unmarshal([]byte(resp), &respMap)
				client.UUID = respMap["uuid"]
				if client.UUID == "" {
					client.Log.Error("No UUID found in registration response")
					time.Sleep(time.Second * 2)
				} else {
					client.Log.Info("Successfully registered with controller. UUID is %s", client.UUID)
					client.Sender.ClientUUID = client.UUID // update UUID of sender object
					client.KeyStoreWriteOut()
				}
			}
		}
	}
}

// VerifyBinary checks client binary hash against configuration
// will attempt to download new binary
func (client *Client) VerifyBinary() bool {
	// check binary hash
	if strings.EqualFold(client.Config.BinaryHash, client.BinaryHash) {
		return true
	} else if !client.Offline {
		// loop forever as successful update will trigger exit
		for {
			client.Log.Info("Client binary hash on disk does not match configuration. Downloading update...")

			// get new binary from control server
			clientBytes, err := client.Sender.GetResource(client.Config.BinaryHash)
			if err != nil {
				client.Log.Error("Unable to retrieve new client binary: %s", err)
				//attempt different controller & proxy combinations
				client.Sender.UpdateConnection(client.Config.ProxyList, client.Config.ControllerList)
				time.Sleep(time.Second * 10)
				continue
			}

			// write new binary to disk
			// add .new extension to avoid locked files -- Nanny will check for .new and replace before restarting
			if err := ioutil.WriteFile(filepath.Join(client.InstallDir, client.InstallName)+".new", clientBytes, 0755); err != nil {
				client.Log.Error("Unable to write new binary file to disk: %s", err)
				time.Sleep(client.PollTime)
				continue
			}

			client.Log.Info("New client binary written to disk. Going for restart...")
			os.Exit(0)
		}
	}
	return false
}

// Initialize sets initial values for client struct values
func (client *Client) Initialize() error {
	var err error

	client.UUID = "" // set UUID to blank to avoid mismatched keys

	//use goInfo to system information
	sysinfo := goInfo.GetInfo()
	client.Hostname = sysinfo.Hostname

	//find interface information
	ifaces, err := net.Interfaces()
	for _, i := range ifaces {
		//skip invalid mac address
		if i.HardwareAddr.String() == "" {
			continue
		}
		if i.HardwareAddr.String()[:8] == "00:00:00" {
			continue
		}
		addrs, _ := i.Addrs()
		// handle err
		for _, addr := range addrs {

			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			client.Interfaces = append(client.Interfaces, map[string]string{"name": i.Name, "ip": ip.String(), "mac": i.HardwareAddr.String()})
		}
	}

	for _, item := range client.Interfaces {
		hosts, err := net.LookupAddr(item["ip"])
		if err != nil {
			client.Log.Error("Error getting FQDN: %v", err)
		} else if len(hosts) == 0 {
			client.Log.Debug("No FQDN found for %s", item["ip"])
		} else {
			client.FQDN = strings.TrimSuffix(hosts[0], ".")
			break // stop looking after we find one
		}
	}

	//default to hostname
	if client.FQDN == "" {
		client.Log.Debug("No FQDN found. Using hostname: %s", client.Hostname)
		client.FQDN = client.Hostname
	} else {
		client.Log.Debug("Using FQDN: %s", client.FQDN)
	}

	//parse domain
	client.Domain = strings.Split(client.FQDN, ".")[0]

	//get arch
	client.Architecture = runtime.GOARCH
	client.Log.Debug("Agent is running on architechure %s", client.Architecture)

	//get os Version
	client.OSVersion = fmt.Sprintf("%v (%v) %v", sysinfo.OS, sysinfo.Kernel, sysinfo.Core)

	client.Log.Debug("Agent is running on OS %s", client.OSVersion)

	//create public and private keys
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		client.Log.Error("Private key cannot be created. %v", err)
	}

	//store keys as pem files
	pubKey := key.PublicKey
	der, err := x509.MarshalPKIXPublicKey(&pubKey)
	if err != nil {
		client.Log.Fatal("Unable to create client's public key: %v", err)
	}

	client.PublicKey = string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: der,
	}))

	// Generate a pem block with the private key
	client.PrivateKey = string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))

	client.Initialized = true // set initialized to true
	//store values to registry
	err = client.KeyStoreWriteOut()

	//log and return any errors
	if err != nil {
		client.Log.Error("Unable to initialize client: %v", err)
		return err
	}

	return nil
}

// KeyStoreWriteOut - write current client struct values to the key store
func (client *Client) KeyStoreWriteOut() error {
	var err error

	//format data to strings
	interfaces, _ := json.Marshal(client.Interfaces)

	//store values in key store
	err = client.LocalDb.KeyStoreInsert("UUID", client.UUID)
	err = client.LocalDb.KeyStoreInsert("InstallDir", client.InstallDir)
	err = client.LocalDb.KeyStoreInsert("IsInitialized", strconv.FormatBool(client.Initialized))
	err = client.LocalDb.KeyStoreInsert("Hostname", client.Hostname)
	err = client.LocalDb.KeyStoreInsert("Domain", client.Domain)
	err = client.LocalDb.KeyStoreInsert("FQND", client.FQDN)
	err = client.LocalDb.KeyStoreInsert("Architecture", client.Architecture)
	err = client.LocalDb.KeyStoreInsert("OSVersion", client.OSVersion)
	err = client.LocalDb.KeyStoreInsert("PublicKey", client.PublicKey)
	err = client.LocalDb.KeyStoreInsert("PrivateKey", client.PrivateKey)
	err = client.LocalDb.KeyStoreInsert("CertName", client.CertName)
	err = client.LocalDb.KeyStoreInsert("LocalDbName", client.LocalDbName)
	err = client.LocalDb.KeyStoreInsert("Interfaces", string(interfaces))
	err = client.LocalDb.KeyStoreInsert("LocalPort", strconv.FormatUint(client.LocalPort, 10))

	return err
}

// KeyStoreReadIn - read key store into client struct
func (client *Client) KeyStoreReadIn() error {
	var err error

	client.UUID, err = client.LocalDb.KeyStoreSelect("UUID")
	client.Hostname, err = client.LocalDb.KeyStoreSelect("Hostname")
	client.Domain, err = client.LocalDb.KeyStoreSelect("Domain")
	client.FQDN, err = client.LocalDb.KeyStoreSelect("FQND")
	client.Architecture, err = client.LocalDb.KeyStoreSelect("Architecture")
	client.OSVersion, err = client.LocalDb.KeyStoreSelect("OSVersion")
	client.PublicKey, err = client.LocalDb.KeyStoreSelect("PublicKey")
	client.PrivateKey, err = client.LocalDb.KeyStoreSelect("PrivateKey")
	client.CertName, err = client.LocalDb.KeyStoreSelect("CertName")
	client.LocalDbName, err = client.LocalDb.KeyStoreSelect("LocalDbName")

	//the following keys require a bit of parsing
	i, err := client.LocalDb.KeyStoreSelect("IsInitialized")
	if err == nil {
		client.Initialized, err = strconv.ParseBool(i)
	}
	interfaces, err := client.LocalDb.KeyStoreSelect("Interfaces")
	if err == nil {
		err = json.Unmarshal([]byte(interfaces), &client.Interfaces)
	}
	LocalPort, err := client.LocalDb.KeyStoreSelect("LocalPort")
	if err == nil {
		client.LocalPort, err = strconv.ParseUint(LocalPort, 10, 64)
	}

	return err
}

// RSAEncrypt - encrypt byte array with the clients RSA keys
func (client *Client) RSAEncrypt(in []byte) ([]byte, error) {
	//return error for empty or nil input
	if in == nil || len(in) == 0 {
		return nil, errors.New("Input cannot be empty or nil")
	}
	//make sure the client has private keys
	if client.PrivateKey == "" {
		return nil, errors.New("Client is missing private key")
	}

	// Extract the PEM-encoded data block
	block, _ := pem.Decode([]byte(client.PrivateKey))
	if block == nil {
		return nil, errors.New("Bad key data: not PEM-encoded")
	}
	if got, want := block.Type, "RSA PRIVATE KEY"; got != want {
		return nil, errors.New("Unknown key type")
	}

	//Decode the RSA private key
	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("Bad private key: " + err.Error())
	}

	out, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, &priv.PublicKey, in, nil)

	return out, err
}

// RSADecrypt decrypts byte array with the clients RSA keys
func (client *Client) RSADecrypt(in []byte) ([]byte, error) {
	//return error for empty or nil input
	if in == nil || len(in) == 0 {
		return nil, errors.New("Input cannot be empty or nil")
	}
	//make sure the client has private keys
	if client.PrivateKey == "" {
		return nil, errors.New("Client is missing private key")
	}

	// Extract the PEM-encoded data block
	block, _ := pem.Decode([]byte(client.PrivateKey))
	if block == nil {
		return nil, errors.New("Bad key data: not PEM-encoded")
	}
	if got, want := block.Type, "RSA PRIVATE KEY"; got != want {
		return nil, errors.New("Unknown key type")
	}

	//Decode the RSA private key
	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("Bad private key: " + err.Error())
	}

	//Decrypt the data
	out, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, priv, in, nil)

	return out, err
}

// GetSHA256 method to get the sha256 of a file given the filepath
func (client *Client) GetSHA256(path string) (string, error) {
	//take hash
	file, err := os.Open(path)
	if err != nil {
		return "", errors.New("Error taking hash of " + path + ": " + err.Error())
	}
	defer file.Close()
	hasher := sha256.New()
	_, err = io.Copy(hasher, file)
	if err != nil {
		return "", errors.New("Error taking hash of " + path + ": " + err.Error())
	}
	//return string version of hashed file
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// Heartbeat - should run as seperate goroutine
func (client *Client) Heartbeat() {
	//write out current time every second
	for range time.Tick(time.Second * 1) {
		timestamp := []byte(fmt.Sprintf("%v", time.Now().UnixNano()))
		if err := ioutil.WriteFile(filepath.Join(client.InstallDir, "heartbeat"), timestamp, 0644); err != nil {
			client.Log.Fatal("Unable to write out heartbeat: %v", err)
		}
	}
}
