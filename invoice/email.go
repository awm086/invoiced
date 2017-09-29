package invoice

import (
	"github.com/julienschmidt/httprouter"
	"net/http"
	"github.com/jung-kurt/gofpdf"
	//"encoding/base64"
	"bytes"
	"github.com/mpdroog/invoiced/writer"
	"github.com/mpdroog/invoiced/config"
	"github.com/mpdroog/invoiced/db"
	"log"
	"gopkg.in/validator.v2"
	"fmt"
	"io"
	"strings"
	"gopkg.in/gomail.v1"
	"os"
)

type Mail struct {
	From string
	Subject string
	To string
	Body string
}

type Job struct {
	To []string
	Subject string
	Text string
	Files []string
}

func Email(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	name := ps.ByName("id")
	bucket := ps.ByName("bucket")
	entity := ps.ByName("entity")
	year := ps.ByName("year")

	if r.Body == nil {
		http.Error(w, "Please send a request body", 400)
		return
	}
	m := new(Mail)
	if e := writer.Decode(r, m); e != nil {
		log.Printf("invoice.Email " + e.Error())
		http.Error(w, "invoice.Email failed to decode input", 400)
		return
	}
	if e := validator.Validate(m); e != nil {
		http.Error(w, fmt.Sprintf("invoice.Email failed validate=%s", e), 400)
		return
	}
	if config.Verbose {
		log.Printf("invoice.Email with id=%s", name)
	}

	paths := []string{
		fmt.Sprintf("%s/%s/%s/sales-invoices-paid/%s.toml", entity, year, bucket, name),
		fmt.Sprintf("%s/%s/%s/sales-invoices-unpaid/%s.toml", entity, year, bucket, name),
	}

	var f *gofpdf.Fpdf
	u := new(Invoice)

	change := db.Commit{
		Name: r.Header.Get("X-User-Name"),
		Email: r.Header.Get("X-User-Email"),
		Message: fmt.Sprintf("Email invoice %s to %s", name, m.To),
	}
	e := db.Update(change, func(t *db.Txn) error {
		e := t.OpenFirst(paths, u)
		if e != nil {
			return e
		}
		f, e = pdf(u)
		if e != nil {
			return e
		}
		// TODO: read hourfile

		buf := new(bytes.Buffer)
		if e := f.Output(buf); e != nil {
			return e
		}

		f, e := t.OpenRaw(u.Meta.HourFile)
		if e != nil {
			return e
		}
		defer f.Close()

		hourbuf := new(bytes.Buffer)
		if _, e := io.Copy(hourbuf, f); e != nil {
			return e
		}

		//enc := base64.StdEncoding
		job := &Job{
			To: strings.Split(m.To, ","),
			Subject: m.Subject,
			Text: m.Body,
			Files: []string{
				paths[1], // TODO: assumption
				u.Meta.HourFile,
			},
		}

		rnd := randStringBytesRmndr(8)
		if e := t.Save(fmt.Sprintf("%s/%s/%s/mails/%s.toml", entity, year, bucket, rnd), true, job); e != nil {
			return e
		}

		hostname, e := os.Hostname()
		if e != nil {
			return e
		}

		conf, ok := config.C.Queues[m.From]
		if !ok {
			return fmt.Errorf("No such from=" + m.From)
		}

		msg := gomail.NewMessage()
		msg.SetHeader("Message-ID", fmt.Sprintf("<%s@%s>", rnd, hostname))
		msg.SetHeader("X-Mailer", "invoiced")
		msg.SetHeader("X-Priority", "3")

		msg.SetHeader("From", fmt.Sprintf("%s <%s>", conf.Display, conf.From))
		msg.SetHeader("Reply-To", fmt.Sprintf("%s <%s>", conf.Display, conf.FromReply))

		msg.SetHeader("To", job.To...)
		msg.SetHeader("Subject", conf.Subject + job.Subject)
		msg.SetBody("text/plain", job.Text)

		msg.Attach(&gomail.File{
			Name: u.Meta.Invoiceid + ".pdf",
			MimeType: "application/pdf",
			Content: buf.Bytes(),
		})
		msg.Attach(&gomail.File{
			Name: "hours.txt",
			MimeType: "plain/text",
			Content: hourbuf.Bytes(),
		})

		if config.Verbose {
			log.Printf("Email=%+v\n", msg)
		}

		mailer := gomail.NewMailer(conf.Host, conf.User, conf.Pass, conf.Port)
		return mailer.Send(msg)
	})
	if e != nil {
		log.Printf("invoice.Email " + e.Error())
		http.Error(w, fmt.Sprintf("invoice.Email failed loading file from disk"), 400)
		return
	}
}