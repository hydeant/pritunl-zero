package subscription

import (
	"net/http"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/hydeant/pritunl-zero/errortypes"
	"github.com/hydeant/pritunl-zero/requires"
)

var (
	Sub    = &Subscription{}
	client = &http.Client{
		Timeout: 30 * time.Second,
	}
)

type Subscription struct {
	Active            bool      `json:"active"`
	Status            string    `json:"status"`
	Plan              string    `json:"plan"`
	Quantity          int       `json:"quantity"`
	Amount            int       `json:"amount"`
	PeriodEnd         time.Time `json:"period_end"`
	TrialEnd          time.Time `json:"trial_end"`
	CancelAtPeriodEnd bool      `json:"cancel_at_period_end"`
	Balance           int64     `json:"balance"`
	UrlKey            string    `json:"url_key"`
}

type subscriptionData struct {
	Active            bool   `json:"active"`
	Status            string `json:"status"`
	Plan              string `json:"plan"`
	Quantity          int    `json:"quantity"`
	Amount            int    `json:"amount"`
	PeriodEnd         int64  `json:"period_end"`
	TrialEnd          int64  `json:"trial_end"`
	CancelAtPeriodEnd bool   `json:"cancel_at_period_end"`
	Balance           int64  `json:"balance"`
	UrlKey            string `json:"url_key"`
}

func Update() (errData *errortypes.ErrorData, err error) {
	sub := &Subscription{}

	sub.Active = true
	sub.Status = "active"
	sub.Plan = "1337"
	sub.Quantity = 1000000
	sub.Amount = 0
	sub.CancelAtPeriodEnd = false
	sub.Balance = 0
	sub.UrlKey = ""
	sub.PeriodEnd = time.Unix(4102448400, 0)

	Sub = sub

	return
}

func update() {
	for {
		time.Sleep(30 * time.Minute)
		err, _ := Update()
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"error": err,
			}).Error("subscription: Update error")
			return
		}
	}
}

func init() {
	module := requires.New("subscription")
	module.After("settings")

	module.Handler = func() (err error) {
		Update()
		go update()
		return
	}
}
