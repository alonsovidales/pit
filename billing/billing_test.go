package billing

import (
	"testing"
)

func TestDebugLevel(t *testing.T) {
	bi := GetBilling(
		"https://api.sandbox.paypal.com/v1/",
		"-wTuNOeVl",
		"eV6y4frwKhDFjcCSO4Bv8NYL2okskviYOP646H8",
		"info-facilitator@pitia.info",
		"Pitia.info",
		"1234 Main St.",
		"Portland",
		"OR",
		"97217",
		"US",
	)

	bi.SendNewBill("test bill", "info@pitia.info", []*InvItem{
		&InvItem{
			Name:     "Test Item",
			Quantity: 10.5,
			UnitPrice: &InvItemPrize{
				Currency: "USD",
				Value:    1,
			},
		},
		&InvItem{
			Name:     "Test Item 2",
			Quantity: 12.5,
			UnitPrice: &InvItemPrize{
				Currency: "USD",
				Value:    2,
			},
		},
	})
}
