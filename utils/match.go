package utils

import (
	"rss-reader/globals"
	"strings"
)

func MatchStr(str string, callback func(string)) {
	for _, v := range globals.MatchList {
		strFinal := strings.ToLower(strings.TrimSpace(str))
		v = strings.ToLower(strings.TrimSpace(v))
		if strings.Contains(strFinal, v) {
			callback(str)
		}
	}
}
