// Package comms contains struct and methods for communicating with the controller
package comms

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"ghost/agent/logger"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const signature = "SIGNATURE"
const userAgent = "GHOSTClient/1.0" //TODO: Make configurable

// Sender struct for sending messages to the controller
type Sender struct {
	ControllerURL     string //Active URL used to contact controller
	Proxy             string //Active Proxy used
	ServerCertificate string //pem string of the ser
	ClientUUID        string
	ClientPrivateKey  string
	ClientPublicKey   string
	Log               *logger.Logger
	uri               string //uri to access on the controller
	message           []byte //Byte array of message to send controller. Caller should serialize json data
	httpClient        *http.Client
	transport         *http.Transport
	mutex             *sync.Mutex
}

// Init method for intializing sender values for first use
func (s *Sender) Init() error {
	//create sender mutex
	s.mutex = &sync.Mutex{}

	//make sure URL can be set
	if s.ControllerURL == "" {
		return errors.New("cannot initialize sender: URL not set")
	}

	//create transport
	s.transport = &http.Transport{
		MaxIdleConns:       1,
		IdleConnTimeout:    1 * time.Second,
		DisableKeepAlives:  true,
		DisableCompression: true, //compression is handled manually
		TLSClientConfig:    &tls.Config{InsecureSkipVerify: true},
		ProxyConnectHeader: http.Header{"User-Agent": []string{userAgent}},
	}

	//create httpClient
	s.httpClient = &http.Client{
		Transport: s.transport,
		Timeout:   120 * time.Second,
	}

	//function to keep headers during redirects
	s.httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return nil
		}
		if len(via) == 0 {
			return nil
		}
		for attr, val := range via[0].Header {
			req.Header[attr] = val
		}

		return nil
	}

	return nil
}

// Send message to controller
// Handles message signing and response verification
// INPUT : message ([]byte), content to send formatted as json string
// INPUT : uri (string), URI only, base URL will be prepended
// OUPUT : response message
func (s *Sender) Send(message []byte, uri string) (string, error) {

	// initialize if needed
	if s.httpClient == nil {
		s.Init()
	}

	// get mutex lock
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// assign message and uri
	s.message = message
	s.uri = uri

	// Create a payload map with json string and signed request
	payload := make(map[string]string)
	payload[signature] = base64.StdEncoding.EncodeToString(s.SignData(s.message))
	payload["jsonString"] = string(s.message)

	//serialize payload structure
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	// set Proxy
	if s.Proxy != "" && strings.ToLower(s.Proxy) != "none" {
		urlI := url.URL{}
		urlProxy, _ := urlI.Parse(s.Proxy)
		s.transport.Proxy = http.ProxyURL(urlProxy)
	} else {
		s.transport.Proxy = nil
	}

	// create request object
	url := fmt.Sprintf("%s/%s/", strings.Trim(s.ControllerURL, "/"), strings.Trim(s.uri, "/"))
	req, _ := http.NewRequest("POST", url, strings.NewReader(string(payloadJSON)))

	// set headers
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	if s.ClientUUID != "" {
		req.Header.Set("client-uuid", s.ClientUUID)
	} else {
		req.Header.Set("client-uuid", "none")
	}

	// make request
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", errors.New("NETWORK ERROR: " + err.Error())
	}
	defer resp.Body.Close()

	// read and parse response
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", errors.New("Unable to read body response: " + err.Error())
	}

	// check for bad status
	if resp.StatusCode != 200 {
		return "", errors.New("Received bad status: " + resp.Status)
	}

	payloadMap := make(map[string]string)
	if err := json.Unmarshal(bodyBytes, &payloadMap); err != nil {
		return "", errors.New("Unable to unmarshal payload map: " + err.Error())
	}

	// check status code
	if resp.StatusCode != http.StatusOK {
		err = errors.New(resp.Status)
	}

	// verify request
	if s.VerifyResponse(payloadMap["jsonString"], payloadMap["SIGNATURE"]) {
		return string(payloadMap["jsonString"]), err
	}

	// Default to returning unverified
	return "", errors.New("unable to verify response signature")
}

// GetResource retrieves a resource file from the control server
func (s *Sender) GetResource(resourceHash string) ([]byte, error) {
	// GetResource retrieves a resource file from the control server
	// INPUT: sha265 hash (string) of resource file to retreive
	// OUTPUT: byte array of resource file.

	// format uri and use send to get resource file
	uri := fmt.Sprintf("/core/resource/%s/", resourceHash)
	respString, err := s.Send([]byte(""), uri)
	if err != nil {
		return []byte(""), err
	}

	// parse response string into map
	respMap := make(map[string]string)
	if err := json.Unmarshal([]byte(respString), &respMap); err != nil {
		return []byte(""), err
	}

	// base64 decode content
	content, err := base64.StdEncoding.DecodeString(respMap["content"])
	if err != nil {
		return []byte(""), err
	}

	return content, nil

}

// VerifyResponse method verifies if the message has been signed by the server
func (s *Sender) VerifyResponse(respStr string, signature string) bool {
	// Get byte arrays for the signature and reponse string
	sigBytes, _ := base64.StdEncoding.DecodeString(signature)

	// take hash response bytes
	respBytes := []byte(respStr)
	hashed := sha256.Sum256(respBytes)

	// construct certifacte
	block, _ := pem.Decode([]byte(s.ServerCertificate))
	if block == nil {
		s.Log.Fatal("Failed to decode PEM block of controller certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		s.Log.Fatal("Failed to parse controller certifact: %v", err)
	}

	rsaPubKey, _ := cert.PublicKey.(*rsa.PublicKey)
	err = rsa.VerifyPKCS1v15(rsaPubKey, crypto.SHA256, hashed[:], sigBytes)
	if err != nil {
		s.Log.Error("Error from signature verification: %s\n", err)
		return false
	}

	return true
}

// SignData method returns signature for inputed data
func (s *Sender) SignData(data []byte) []byte {
	//hash data
	hashed := sha256.Sum256(data)

	//parse private key
	block, _ := pem.Decode([]byte(s.ClientPrivateKey))
	if block == nil {
		s.Log.Fatal("Failed to decode PEM block of controller certificate")
	}

	rsaPrivateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		s.Log.Fatal("Unable to parse private key: %v", err)
	}

	signature, err := rsa.SignPKCS1v15(rand.Reader, rsaPrivateKey, crypto.SHA256, hashed[:])
	if err != nil {
		s.Log.Fatal("Error from signing: %s\n", err)
	}

	return signature
}

// UpdateConnection test controller URLs and proxies round robin until one works or all fail
// INPUT proxyList : list of Proxy address
// INPUT controllerList :
// OUTPUT bool : true if working connection found
func (s *Sender) UpdateConnection(proxyList, controllerList []string) bool {

	// store original settings
	oldProxy := s.Proxy
	oldcontrollerURL := s.ControllerURL

	// double loop iteration through contoller and Proxy combinations
	for _, ControllerURL := range controllerList {
		s.ControllerURL = ControllerURL

		// test with default Proxy first
		s.Log.Debug("Testing Controller URL %v with Proxy %v", s.ControllerURL, s.Proxy)
		if s.TestConnection() {
			s.Log.Info("Updating network sender to use controller URL: %v and Proxy: %v", s.ControllerURL, s.Proxy)
			return true
		}

		// test with no Proxy if not default
		if s.Proxy != "" && strings.ToLower(s.Proxy) != "none" {
			s.Proxy = ""
			s.Log.Debug("Testing Controller URL %v with No Proxy", s.ControllerURL)
			if s.TestConnection() {
				s.Log.Info("Updating network sender to use controller URL: %v and No Proxy", s.ControllerURL)
				return true
			}
		}

		// run through the Proxy list
		for _, Proxy := range proxyList {
			s.Proxy = Proxy
			s.Log.Debug("Testing Controller URL %v with Proxy %v", s.ControllerURL, s.Proxy)
			if s.TestConnection() {
				s.Log.Info("Updating network sender to use controller URL: %v and Proxy: %v", s.ControllerURL, s.Proxy)
				return true
			}
		}
	}

	// nothing worked set back to old settings and return
	s.Proxy = oldProxy
	s.ControllerURL = oldcontrollerURL
	return false
}

// TestConnection checks if the current Sender settings can connect to controller
// Returns true if connection is successful
func (s *Sender) TestConnection() bool {

	//set Proxy
	if s.Proxy != "" && strings.ToLower(s.Proxy) != "none" {
		urlI := url.URL{}
		urlProxy, _ := urlI.Parse(s.Proxy)
		s.transport.Proxy = http.ProxyURL(urlProxy)
	} else {
		s.transport.Proxy = nil
	}

	//create request object
	s.ControllerURL = strings.TrimSuffix(s.ControllerURL, "/")
	req, err := http.NewRequest("GET", s.ControllerURL+"/core/conntest/", nil)
	if err != nil {
		s.Log.Fatal("Error creating request: %v", err)
	}

	//set user-agent string
	req.Header.Set("User-Agent", userAgent)

	//make request
	resp, err := s.httpClient.Do(req)
	if err != nil {
		s.Log.Debug("NETWORK ERROR: %v", err) // DEBUG
		return false
	}
	defer resp.Body.Close()

	//read and parse response
	body, _ := ioutil.ReadAll(resp.Body)
	var respMap map[string]string
	if err := json.Unmarshal(body, &respMap); err != nil {
		s.Log.Debug("Invalid Response (unable to deserialize)")
		return false
	}

	if respMap["status"] == "success" {
		return true
	}
	return false
}

// Get sends a basic unauthenticated get request
// Even though the get request is unathenticated, this function still expects signature
// INPUT : uri (string), URI only, base URL will be prepended
// OUTPUT : response message
// OUTPUT : error; including non-200 responses
func (s *Sender) Get(uri string) (string, error) {

	// initialize if needed
	if s.httpClient == nil {
		s.Init()
	}

	// get mutex lock
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// set uri
	s.uri = uri

	//set Proxy
	if s.Proxy != "" && strings.ToLower(s.Proxy) != "none" {
		urlI := url.URL{}
		urlProxy, _ := urlI.Parse(s.Proxy)
		s.transport.Proxy = http.ProxyURL(urlProxy)
	} else {
		s.transport.Proxy = nil
	}

	//create request object
	url := fmt.Sprintf("%s/%s/", strings.Trim(s.ControllerURL, "/"), strings.Trim(s.uri, "/"))
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		s.Log.Fatal("Error creating request: %v", err)
	}

	//set user-agent string
	req.Header.Set("User-Agent", userAgent)

	//make request
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", errors.New("NETWORK ERROR: " + err.Error())
	}
	defer resp.Body.Close()

	// read and parse response
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", errors.New("Unable to read body response: " + err.Error())
	}

	// check for bad status
	if resp.StatusCode != 200 {
		return "", errors.New("Received bad status: " + resp.Status)
	}

	payloadMap := make(map[string]string)
	if err := json.Unmarshal(bodyBytes, &payloadMap); err != nil {
		return "", errors.New("Unable to unmarshal payload map: " + err.Error())
	}

	// check status code
	if resp.StatusCode != http.StatusOK {
		err = errors.New(resp.Status)
	}

	// verify request
	if s.VerifyResponse(payloadMap["jsonString"], payloadMap["SIGNATURE"]) {
		return string(payloadMap["jsonString"]), err
	}

	// Default to returning unverified
	return "", errors.New("unable to verify response signature")
}
