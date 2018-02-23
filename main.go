package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"

	"bytes"
	"encoding/json"
	"github.com/bitrise-io/go-utils/log"
	"io/ioutil"
	"net/http"
	"strings"
)

// -----------------------
// --- Models
// -----------------------

// ConfigsModel ...
type ConfigsModel struct {
	JiraUsername     string
	JiraPassword     string
	JiraInstanceURL  string
	IssueIDOrKeyList []string
	FieldKey         string
	FieldValue       string
}

type Cookie struct {
	Session struct {
		Name 	string `json:"name"`
		Value 	string `json:"value"`
	} `json:"session"`
	LoginInfo struct {
		FailedLoginCount 	int `json:"failedLoginCount"`
		LoginCount 			int `json:"loginCount"`
		LastFailedLoginTime string `json:"lastFailedLoginTime"`
		PreviousLoginTime 	string `json:"previousLoginTime"`
	} `json:"loginInfo"`
}

func main() {
	configs := createConfigsModelFromEnvs()

	configs.dump()
	if err := configs.validate(); err != nil {
		log.Errorf("Issue with input: %s", err)
		os.Exit(1)
	}

	if err := performRequests(configs); err != nil {
		log.Errorf("Could not update issue, error: %s", err)
		os.Exit(2)
	}
}

func createCookie(configs ConfigsModel, body []byte) (*Cookie, error) {
	payload := map[string]interface{}{
		"username": configs.JiraUsername,
		"password": configs.JiraPassword,
	}
	cookiePayload, _ := json.Marshal(payload)

	requestURL := fmt.Sprintf("%s/rest/auth/latest/session", configs.JiraInstanceURL)
	request, err := http.NewRequest("POST", requestURL, bytes.NewBuffer(cookiePayload))
	if err != nil {
		return nil, err
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	client := http.Client{}
	response, err := client.Do(request)

	defer response.Body.Close()
	responseBody, err := ioutil.ReadAll(response.Body)

	var cookie = new(Cookie)
	err = json.Unmarshal(responseBody, &cookie)

	if err != nil {
		return cookie , err
	}

	return cookie, err
}

func createRequestBody(configs ConfigsModel) ([]byte, error) {
	payload := map[string]interface{}{
		"fields": map[string]interface{}{
			configs.FieldKey: configs.FieldValue,
		},
	}
	return json.Marshal(payload)
}

func createRequest(configs ConfigsModel, issueIDOrKey string, body []byte) (*http.Request, error) {
	requestURL := fmt.Sprintf("%s/rest/api/2/issue/%s", configs.JiraInstanceURL, issueIDOrKey)
	request, err := http.NewRequest("PUT", requestURL, bytes.NewBuffer(body))
	if err != nil {
		return request, err
	}

	cookie, err := createCookie(configs, body)
	if err != nil {
		return request, err
	}

	cookieVal := cookie.Session.Name + "=" + cookie.Session.Value

	request.Header.Set("Cookie", cookieVal)
	request.Header.Set("Content-Type", "application/json")
	return request, nil
}

func updateIssue(configs ConfigsModel, issueIDOrKey string, body []byte) error {
	log.Infof("Updating issue %s", issueIDOrKey)

	request, err := createRequest(configs, issueIDOrKey, body)
	if err != nil {
		return err
	}

	client := http.Client{}
	response, err := client.Do(request)

	if err != nil {
		return err
	}

	defer func() {
		err := response.Body.Close()
		if err != nil {
			log.Warnf("Failed to close response body, error: %s", err)
		}
	}()

	if response.StatusCode != http.StatusNoContent {
		log.Warnf("JIRA API response status: %s", response.Status)
		contents, readErr := ioutil.ReadAll(response.Body)
		if readErr != nil {
			return errors.New("could not read JIRA API response")
		}
		if response.Header.Get("X-Seraph-LoginReason") == "AUTHENTICATION_DENIED" {
			log.Warnf("CAPTCHA triggered")
		} else {
			log.Warnf("JIRA API response: %s", contents)
		}
		return errors.New("JIRA API request failed")
	}

	log.Infof("Issue %s updated successfully", issueIDOrKey)
	return nil
}

func performRequests(configs ConfigsModel) error {
	body, err := createRequestBody(configs)
	if err != nil {
		return err
	}

	for _, idOrKey := range configs.IssueIDOrKeyList {
		if err := updateIssue(configs, idOrKey, body); err != nil {
			return err
		}
	}

	return nil
}

func createConfigsModelFromEnvs() ConfigsModel {
	configs := ConfigsModel{
		JiraUsername:     os.Getenv("jira_username"),
		JiraPassword:     os.Getenv("jira_password"),
		JiraInstanceURL:  os.Getenv("jira_instance_url"),
		IssueIDOrKeyList: strings.Split(os.Getenv("issue_id_or_key_list"), "|"),
		FieldKey:         os.Getenv("field_key"),
		FieldValue:       os.Getenv("field_value"),
	}
	for i, idOrKey := range configs.IssueIDOrKeyList {
		configs.IssueIDOrKeyList[i] = strings.TrimSpace(idOrKey)
	}
	return configs
}

func (configs ConfigsModel) dump() {
	fmt.Println()
	log.Infof("Configs:")
	log.Printf(" - JiraUsername: %s", configs.JiraUsername)
	log.Printf(" - JiraPassword (hidden): %s", strings.Repeat("*", 5))
	log.Printf(" - JiraInstanceURL: %s", configs.JiraInstanceURL)
	log.Printf(" - IssueIdOrKeyList: %v", configs.IssueIDOrKeyList)
	log.Printf(" - FieldKey: %s", configs.FieldKey)
	log.Printf(" - FieldValue: %s", configs.FieldValue)
}

func (configs ConfigsModel) validate() error {
	if configs.JiraUsername == "" {
		return errors.New("no Jira Username specified")
	}
	if configs.JiraPassword == "" {
		return errors.New("no Jira Password specified")
	}
	_, err := url.ParseRequestURI(configs.JiraInstanceURL)
	if err != nil {
		return fmt.Errorf("invalid Jira instance URL, error %s", err)
	}
	if len(configs.IssueIDOrKeyList) == 0 {
		return errors.New("no Jira issue IDs nor keys specified")
	}
	for i, idOrKey := range configs.IssueIDOrKeyList {
		if idOrKey == "" {
			return fmt.Errorf("empty Jira issue ID nor key specified at index %d", i)
		}
	}
	if configs.FieldKey == "" {
		return errors.New("no field key specified")
	}
	return nil
}
