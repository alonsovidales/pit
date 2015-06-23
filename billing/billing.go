package billing

import (
	"encoding/json"
	"fmt"
	"github.com/alonsovidales/pit/log"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
)

type Billing struct {
	token           string
	tokenExpireTs   int64
	url             string
	baseUri         string
	clientId        string
	secret          string
	email           string
	bussinessName   string
	addrAddr        string
	addrCity        string
	addrState       string
	addrZip         string
	addrCountryCode string
}

type InvMerchantInfo struct {
	Email        string            `json:"email"`
	BusinessName string            `json:"business_name"`
	Address      map[string]string `json:"address"`
}

type InvItemPrize struct {
	Currency string `json:"currency"`
	Value    int    `json:"value"`
}

type InvItem struct {
	Name      string        `json:"name"`
	Quantity  float64       `json:"quantity"`
	UnitPrice *InvItemPrize `json:"unit_price"`
}

type Invoice struct {
	MerchantInfo *InvMerchantInfo    `json:"merchant_info"`
	BillingInfo  []map[string]string `json:"billing_info"`
	Items        []*InvItem          `json:"items"`
	Note         string              `json:"note"`
}

func GetBilling(baseUri, clientId, secret, email, bussinessName, addrAddr, addrCity, addrState, addrZip, addrCountryCode string) (bi *Billing) {
	bi = &Billing{
		baseUri:         baseUri,
		clientId:        clientId,
		secret:          secret,
		email:           email,
		bussinessName:   bussinessName,
		addrAddr:        addrAddr,
		addrCity:        addrCity,
		addrState:       addrState,
		addrZip:         addrZip,
		addrCountryCode: addrCountryCode,
	}

	return
}

func (bi *Billing) SendNewBill(invTitle, targetEmail string, items []*InvItem) (billId string, err error) {
	inv := &Invoice{
		MerchantInfo: &InvMerchantInfo{
			Email:        bi.email,
			BusinessName: bi.addrAddr,
			Address: map[string]string{
				"line1":        bi.addrAddr,
				"city":         bi.addrCity,
				"state":        bi.addrState,
				"postal_code":  bi.addrZip,
				"country_code": bi.addrCountryCode,
			},
		},
		BillingInfo: []map[string]string{
			map[string]string{
				"email": targetEmail,
			},
		},
		Items: items,
		Note:  invTitle,
	}

	invStr, _ := json.Marshal(inv)

	if body, err := bi.callToApi("invoicing/invoices", "POST", string(invStr)); err == nil {
		responseInfo := make(map[string]interface{})
		err = json.Unmarshal(body, &responseInfo)
		if err != nil {
			log.Error("Problem trying to response from PayPal API, Error:", err)
		}

		billId = responseInfo["id"].(string)
		sendRes, err := bi.callToApi(fmt.Sprintf("invoicing/invoices/%s/send", billId), "POST", "")

		log.Info("Sent!!!:", fmt.Sprintf("invoicing/invoices/%s/send", billId), string(sendRes), err)
	}

	return
}

func (bi *Billing) getToken() {
	client := &http.Client{}

	req, err := http.NewRequest("POST", bi.baseUri+"oauth2/token", strings.NewReader("grant_type=client_credentials"))
	req.Header.Add("Accept", `application/json`)
	req.Header.Add("Accept-Language", `en_US`)
	req.SetBasicAuth(bi.clientId, bi.secret)
	resp, err := client.Do(req)
	if err != nil {
		log.Error("Problem trying to request a new token from the PayPal API, Error:", err)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error("Problem trying to parse the response form the PayPal API, Error:", err)
	}

	responseInfo := make(map[string]interface{})
	err = json.Unmarshal(body, &responseInfo)
	if err != nil {
		log.Error("Problem trying to read token from PayPal API, Error:", err)
	}

	bi.token = responseInfo["access_token"].(string)
	bi.tokenExpireTs = time.Now().Unix() + int64(responseInfo["expires_in"].(float64))
}

func (bi *Billing) callToApi(uri string, method string, reqBody string) (body []byte, err error) {
	if time.Now().Unix() > bi.tokenExpireTs {
		bi.getToken()
	}

	client := &http.Client{}

	url := bi.baseUri + uri
	log.Info(reqBody)
	req, err := http.NewRequest(method, url, strings.NewReader(reqBody))
	req.Header.Add("Content-Type", `application/json`)
	req.Header.Add("Authorization", "Bearer "+bi.token)
	resp, err := client.Do(req)
	if err != nil {
		log.Error("Problem trying to do a request to the PayPal API, Url:", url, "Error:", err)
	}

	body, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error("Problem trying to parse the response form the PayPal API, Error:", err)
	}

	return
}
