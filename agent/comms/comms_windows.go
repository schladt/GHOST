//Package contains struct and methods for communicating with the controller
//Windows specific comms functions

package comms

import (
	"io/ioutil"
	"net/http"
	"regexp"
	"strings"

	"golang.org/x/sys/windows/registry"
)

//Find system proxies by searching the registry
//OUTPUT : list of proxies found on the system  ([]string)
func FindProxies() ([]string, error) {

	//get all user profiles for this system
	users, err := registry.USERS.ReadSubKeyNames(1024)

	var proxies []string

	tempProxies := make(map[string]struct{})
	tempPacFiles := make(map[string]struct{})

	for _, userName := range users {
		k, err := registry.OpenKey(registry.USERS, userName+"\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings", registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		defer k.Close()

		proxyServer, _, _ := k.GetStringValue("ProxyServer")
		pacFile, _, _ := k.GetStringValue("AutoConfigUrl")

		if proxyServer != "" {
			tempProxies[proxyServer] = struct{}{}
		}

		if pacFile != "" {
			tempPacFiles[pacFile] = struct{}{}
		}
	}

	//process pacfiles
	for pacFile := range tempPacFiles {
		resp, err := http.Get(pacFile)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		//read and parse response
		body, _ := ioutil.ReadAll(resp.Body)

		re := regexp.MustCompile("\"PROXY\\s(.*?)\"")
		matches := re.FindAllSubmatch(body, -1)

		for _, match := range matches {
			Proxy := string(match[1])
			if Proxy != "" && strings.ToLower(Proxy) != "none" {
				tempProxies[Proxy] = struct{}{}
			}
		}
	}

	//convert the temp maps into proper slices
	for Proxy := range tempProxies {
		if len(Proxy) < 7 {
			Proxy = "http://" + Proxy
		} else if Proxy[:7] != "http://" {
			Proxy = "http://" + Proxy
		}
		proxies = append(proxies, Proxy)
	}

	return proxies, err
}
