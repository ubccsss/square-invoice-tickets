package square

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"sync"
)

var setupOnce sync.Once

func makeRequest(path string, body interface{}) ([]byte, int, error) {
	url := "https://api.squareup.com" + path

	log.Printf("Hitting %s", url)

	loginResp, err := http.Get("https://squareup.com/login")
	if err != nil {
		return nil, 0, err
	}
	loginResp.Body.Close()

	jsonStr, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonStr))
	req.Header.Set("Content-Type", "application/json")
	cookies := http.DefaultClient.Jar.Cookies(req.URL)
	csrf := ""
	for _, cookie := range cookies {
		if cookie.Name == "_js_csrf" {
			csrf = cookie.Value
		}
	}
	req.Header.Set("X-Csrf-Token", csrf)
	req.Header.Set("Host", "api.squareup.com")
	req.Header.Set("Origin", "https://squareup.com")
	req.Header.Set("Referer", "https://squareup.com/login")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/47.0.2526.111 Safari/537.36")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	buf, _ := ioutil.ReadAll(resp.Body)
	return buf, resp.StatusCode, nil
}

func Setup() {
	setupOnce.Do(func() {
		jar, err := cookiejar.New(nil)
		if err != nil {
			log.Fatal(err)
		}
		http.DefaultClient.Jar = jar
	})
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func Login(email, pass string) error {
	Setup()

	req := LoginRequest{email, pass}
	body, code, err := makeRequest("/mp/login", &req)
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("error logging into square %d, %s", code, body)
	}
	return nil
}
