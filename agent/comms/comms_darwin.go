//Package contains struct and methods for communicating with the controller
//Linux specific comms functions
package comms

import (
	"os"
)

//OUTPUT : list of proxies found on the system  ([]string)
func FindProxies() ([]string, error) {
	var proxies []string

	//map used to de-dupe proxies
	tempProxies := make(map[string]struct{})
	tempProxies[os.Getenv("http_proxy")] = struct{}{}
	tempProxies[os.Getenv("https_proxy")] = struct{}{}
	tempProxies[os.Getenv("HTTP_PROXY")] = struct{}{}
	tempProxies[os.Getenv("HTTPS_PROXY")] = struct{}{}

	//convert and format the temp maps into proper slices
	for Proxy, _ := range tempProxies {
		//skip blank proxies
		if len(Proxy) == 0 {
			continue
		}

		//Add http prefix if needed
		if len(Proxy) < 7 {
			Proxy = "http://" + Proxy
		} else if Proxy[:7] != "http://" && Proxy[:8] != "https://" {
			Proxy = "http://" + Proxy
		}
		proxies = append(proxies, Proxy)
	}

	return proxies, nil
}
