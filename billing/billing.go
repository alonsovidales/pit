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

// Billing Structure used to manage all the communication with the billing
// provider, in this case PayPal
type Billing struct {
	token           string
	tokenExpireTs   int64
	url             string
	baseURI         string
	clientID        string
	secret          string
	email           string
	bussinessName   string
	addrAddr        string
	addrCity        string
	addrState       string
	addrZip         string
	addrCountryCode string
}

// InvMerchantInfo Information of the client
type InvMerchantInfo struct {
	Email        string            `json:"email"`
	BusinessName string            `json:"business_name"`
	Address      map[string]string `json:"address"`
}

// InvItemPrize Price for the item to be billed
type InvItemPrize struct {
	Currency string `json:"currency"`
	Value    int    `json:"value"`
}

// InvItem Item name and quantity
type InvItem struct {
	Name      string        `json:"name"`
	Quantity  float64       `json:"quantity"`
	UnitPrice *InvItemPrize `json:"unit_price"`
}

// Invoice Information for an invoice
type Invoice struct {
	MerchantInfo *InvMerchantInfo    `json:"merchant_info"`
	BillingInfo  []map[string]string `json:"billing_info"`
	Items        []*InvItem          `json:"items"`
	Note         string              `json:"note"`
}

// GetBilling Returns an object that allows to manage all the information of
// the billing provider
func GetBilling(baseURI, clientID, secret, email, bussinessName, addrAddr, addrCity, addrState, addrZip, addrCountryCode string) (bi *Billing) {
	bi = &Billing{
		baseURI:         baseURI,
		clientID:        clientID,
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

// SendNewBill Generates and sends a new bill to the provider to be processed
func (bi *Billing) SendNewBill(invTitle, targetEmail string, items []*InvItem) (billID string, err error) {
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

	if body, err := bi.callToAPI("invoicing/invoices", "POST", string(invStr)); err == nil {
		responseInfo := make(map[string]interface{})
		err = json.Unmarshal(body, &responseInfo)
		if err != nil {
			log.Error("Problem trying to response from PayPal API, Error:", err)
		}

		billID = responseInfo["id"].(string)
		sendRes, err := bi.callToAPI(fmt.Sprintf("invoicing/invoices/%s/send", billID), "POST", "")

		log.Info("Sent!!!:", fmt.Sprintf("invoicing/invoices/%s/send", billID), string(sendRes), err)
	}

	return
}

// getToken Retreves and returns the token from the provider
func (bi *Billing) getToken() {
	client := &http.Client{}

	req, err := http.NewRequest("POST", bi.baseURI+"oauth2/token", strings.NewReader("grant_type=client_credentials"))
	req.Header.Add("Accept", `application/json`)
	req.Header.Add("Accept-Language", `en_US`)
	req.SetBasicAuth(bi.clientID, bi.secret)
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

// callToAPI Performs an HTTP request to the billing provider API
func (bi *Billing) callToAPI(uri string, method string, reqBody string) (body []byte, err error) {
	if time.Now().Unix() > bi.tokenExpireTs {
		bi.getToken()
	}

	client := &http.Client{}

	url := bi.baseURI + uri
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
