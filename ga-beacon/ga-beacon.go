package beacon

import (
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"crypto/rand"
	"encoding/hex"

	"appengine"
	"appengine/delay"
	"appengine/urlfetch"
)

const beaconURL = "http://www.google-analytics.com/collect"

var (
	pixel        = mustReadFile("static/pixel.gif")
	badge        = mustReadFile("static/badge.svg")
	pageTemplate = template.Must(template.New("page").ParseFiles("ga-beacon/page.html"))
)

func init() {
	http.HandleFunc("/", handler)
}

func mustReadFile(path string) []byte {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return b
}

func generateUUID(cid *string) error {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return err
	}

	b[8] = (b[8] | 0x80) & 0xBF // what's the purpose ?
	b[6] = (b[6] | 0x40) & 0x4F // what's the purpose ?
	*cid = hex.EncodeToString(b)
	return nil
}

var delayHit = delay.Func("collect", logHit)

func logHit(c appengine.Context, params []string, ua string, cid string) error {
	// https://developers.google.com/analytics/devguides/collection/protocol/v1/reference
	payload := url.Values{
		"v":   {"1"},        // protocol version = 1
		"t":   {"pageview"}, // hit type
		"tid": {params[0]},  // tracking / property ID
		"cid": {cid},        // unique client ID (server generated UUID)
		"dp":  {params[1]},  // page path
	}

	req, _ := http.NewRequest("POST", beaconURL, strings.NewReader(payload.Encode()))
	req.Header.Add("User-Agent", ua)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	if resp, err := urlfetch.Client(c).Do(req); err != nil {
		c.Errorf("GA collector POST error: %s", err.Error())
		return err
	} else {
		c.Debugf("GA collector status: %v, cid: %v", resp.Status, cid)
	}
	return nil
}

func handler(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	params := strings.SplitN(strings.Trim(r.URL.Path, "/"), "/", 2)

	// / -> redirect
	if len(params[0]) == 0 {
		http.Redirect(w, r, "https://github.com/igrigorik/ga-beacon", http.StatusFound)
		return
	}

	// /account -> account template
	if len(params) == 1 {
		templateParams := struct {
			Account string
		}{
			Account: params[0],
		}
		if err := pageTemplate.ExecuteTemplate(w, "page.html", templateParams); err != nil {
			http.Error(w, "could not show account page", 500)
			c.Errorf("Cannot execute template: %v", err)
		}
		return
	}

	// /account/page -> GIF + log pageview to GA collector
	var cid string
	if cookie, err := r.Cookie("cid"); err != nil {
		if err := generateUUID(&cid); err != nil {
			c.Debugf("Failed to generate client UUID: %v", err)
		} else {
			c.Debugf("Generated new client UUID: %v", cid)
			http.SetCookie(w, &http.Cookie{Name: "cid", Value: cid, Path: fmt.Sprint("/", params[0])})
		}
	} else {
		cid = cookie.Value
		c.Debugf("Existing CID found: %v", cid)
	}

	if len(cid) != 0 {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("CID", cid)

		logHit(c, params, r.Header.Get("User-Agent"), cid)
		// delayHit.Call(c, params, r.Header.Get("User-Agent"), cid)
	}

	// Write out GIF pixel or badge, based on presence of "pixel" param.
	query, _ := url.ParseQuery(r.URL.RawQuery)
	if _, ok := query["pixel"]; ok {
		w.Header().Set("Content-Type", "image/gif")
		w.Write(pixel)
	} else {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write(badge)
	}
}
