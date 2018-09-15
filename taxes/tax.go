package taxes

import (
	"fmt"
	"net/http"
	"github.com/julienschmidt/httprouter"
	"github.com/shopspring/decimal"
	"github.com/mpdroog/invoiced/config"
	"github.com/mpdroog/invoiced/db"
	"github.com/mpdroog/invoiced/invoice"
	"log"
	"github.com/mpdroog/invoiced/writer"
	"strings"
)

type Sum struct {
	Ex string
	Tax string
	EUEx string  // TOOODOOO
}

func addValue(sum, add string, dec int) (string, error) {
	if sum == "" {
		sum = "0.00"
	}

	s, e := decimal.NewFromString(sum)
	if e != nil {
		return sum, e
	}

	a, e := decimal.NewFromString(add)
	if e != nil {
		return sum, e
	}
	return s.Add(a).StringFixed(int32(dec)), nil
}


func Tax(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	entity := ps.ByName("entity")
	year := ps.ByName("year")
	quarter := ps.ByName("quarter")

	sum := &Sum{}

	e := db.View(func(t *db.Txn) error {
		// invoice
		paths := []string{
			fmt.Sprintf("%s/%s/%s/sales-invoices-paid", entity, year, quarter),
			fmt.Sprintf("%s/%s/%s/sales-invoices-unpaid", entity, year, quarter),
		}
		u := new(invoice.Invoice)
		_, e := t.List(paths, db.Pagination{From:0, Count:0}, &u, func(filename, filepath, path string) error {
			var e error
			if config.Verbose {
				log.Printf("Invoice(%s) total=%s ex=%s", u.Meta.Invoiceid, u.Total.Total, u.Total.Ex)
			}
			if strings.Contains(u.Notes, "VAT Reverse charge") {
				sum.EUEx, e = addValue(sum.EUEx, u.Total.Ex, 2)
			} else {
				sum.Ex, e = addValue(sum.Ex, u.Total.Ex, 2)
			}
			if e != nil {
				return e
			}

			sum.Tax, e = addValue(sum.Tax, u.Total.Tax, 2)
			return e
		})
		return e
	})
	if e != nil {
		panic(e)
	}

	sum.EUEx, e = addValue(sum.EUEx, "0", 0)
	sum.Ex, e = addValue(sum.Ex, "0", 0)
	sum.Tax, e = addValue(sum.Tax, "0", 0)	

	if e := writer.Encode(w, r, sum); e != nil {
		log.Printf("taxes.Tax " + e.Error())
	}
}