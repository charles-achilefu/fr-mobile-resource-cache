package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/couchbaselabs/logg"
)

//AuthResponse response from auth service
type AuthResponse struct {
	SessionID  string `json:"session_id"`
	Expires    string `json:"expires"`
	CookieName string `json:"cookie_name"`
}

var globalHTTP = &http.Client{}

func readResource(url string) ([]byte, error) {
	logg.LogTo(TagLog, "Getting %s\n", url)

	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	document, err := ioutil.ReadAll(res.Body)
	res.Body.Close()

	return document, err
}

// getDocument queries a document via sync gateway's REST API
// and returns the document contents and last revision
func getDocument(documentID string) ([]byte, string, error) {

	result, rev, err := getRawDocument(documentID)
	if err != nil {
		return result, rev, err
	}

	//cleanup the remote document
	result, err = cleanupSyncDocument(result)
	return result, rev, err
}

func getRawDocument(documentID string) ([]byte, string, error) {
	var syncEndpoint = getSyncEndpoint() + documentID

	result, err := readResource(syncEndpoint)

	var jsonObject map[string]interface{}
	err = json.Unmarshal(result, &jsonObject)

	if err != nil {
		return nil, "", err
	}

	rev, _ := jsonObject["_rev"].(string)

	return result, rev, nil
}

func postDocument(document []byte, documentID string) (string, error) {
	var syncEndpoint = getSyncEndpoint() + documentID

	_, rev, err := getDocument(documentID)

	if rev != "" {
		syncEndpoint += "?rev=" + rev
	}

	request, err := http.NewRequest("PUT", syncEndpoint, bytes.NewReader(document))
	request.ContentLength = int64(len(document))
	setAuth(request)

	logRequest(request)

	response, err := globalHTTP.Do(request)

	if err != nil {
		return "", err
	}

	defer response.Body.Close()
	contents, err := ioutil.ReadAll(response.Body)

	if err != nil {
		return "", err
	}

	logg.LogTo(TagLog, "%s", contents)

	var jsonObject map[string]interface{}
	err = json.Unmarshal(contents, &jsonObject)

	if err != nil {
		return "", err
	}

	rev, ok := jsonObject["rev"].(string)

	if ok {
		return rev, err
	}

	return "", nil
}

func deleteDocument(documentID string) error {
	var syncEndpoint = getSyncEndpoint() + documentID

	_, rev, err := getDocument(documentID)

	if rev != "" {
		syncEndpoint += "?rev=" + rev
	}

	request, err := http.NewRequest("DELETE", syncEndpoint, nil)
	setAuth(request)

	logRequest(request)

	response, err := globalHTTP.Do(request)

	if err != nil {
		return err
	}

	defer response.Body.Close()

	return err
}

func postAttachment(fileContents []byte, parentDoc string, documentName string) error {
	var syncEndpoint = getSyncEndpoint() + parentDoc + "/" + documentName

	request, err := http.NewRequest("PUT", syncEndpoint, bytes.NewReader(fileContents))
	request.Header.Add("Content-Type", http.DetectContentType(fileContents))
	setAuth(request)

	logg.LogTo(TagLog, "%s", syncEndpoint)

	response, err := globalHTTP.Do(request)

	defer response.Body.Close()

	logg.LogTo(TagLog, "Post status code: %v", response.StatusCode)

	return err
}

func setAuth(request *http.Request) {
	if authConfig.Username != "" && authConfig.Password != "" {
		if authConfig.SimpleAuth {
			request.SetBasicAuth(authConfig.Username, authConfig.Password)
		} else {
			session := authenticate()
			layout := "2006-01-02T15:04:05Z07:00"
			expire, err := time.Parse(layout, session.Expires)
			if err != nil {
				logg.LogPanic("Error parsing time: %v", err)
			}

			rawCookie := []string{session.CookieName + "=" + session.SessionID}
			maxAge := 0
			secure := true
			httpOnly := true
			path := "/"

			cookie := http.Cookie{session.CookieName, session.SessionID, path, config.SyncURL, expire, expire.Format(time.UnixDate), maxAge, secure, httpOnly, rawCookie[0], rawCookie}

			request.AddCookie(&cookie)
		}
	}

}

//authenticate uses a custom service to authenticate against a credentials repository like Active Directory
//and returns a session from sync gateway
func authenticate() AuthResponse {
	request, err := http.NewRequest("POST", authConfig.ServerURL, bytes.NewReader([]byte("{\"username\": \""+authConfig.Username+"\", \"password\": \""+authConfig.Password+"\"}")))

	if err != nil {
		logg.LogPanic("Error creating request: %v", err)
	}

	logRequest(request)

	response, err := globalHTTP.Do(request)
	if err != nil {
		logg.LogPanic("Error authenticating: %v", err)
	}

	defer response.Body.Close()

	authResponse := AuthResponse{}

	document, err := ioutil.ReadAll(response.Body)
	if err != nil {
		logg.LogPanic("Error reading contents: %v", err)
	}

	json.Unmarshal(document, &authResponse)

	return authResponse
}

func getAttachmentDigest(documentID, attachment string) (string, error) {
	doc, _, err := getRawDocument(documentID)
	if err != nil {
		return "", err
	}

	var jsonObject map[string]interface{}
	err = json.Unmarshal(doc, &jsonObject)

	if err != nil {
		return "", err
	}

	att, _ := jsonObject["_attachments"].(map[string]interface{})
	attContent, _ := att[attachment].(map[string]interface{})

	result, _ := attContent["digest"].(string)

	return result, err
}
