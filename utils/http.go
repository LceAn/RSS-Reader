package utils

import (
	"errors"
	"net/url"
)

func endpointForLog(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return "<invalid>"
	}
	return parsed.Host
}

// networkError removes url.Error.URL, which can contain bot tokens or signed
// feed parameters, while retaining the underlying diagnostic.
func networkError(err error) error {
	var urlError *url.Error
	if errors.As(err, &urlError) {
		return urlError.Err
	}
	return err
}
