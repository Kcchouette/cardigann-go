package login

import "net/http/cookiejar"

func newJarForTest() (*cookiejar.Jar, error) {
	return cookiejar.New(nil)
}
