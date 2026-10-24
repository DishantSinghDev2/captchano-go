// Command example solves Google's public reCAPTCHA v2 demo widget and prints
// the token. Run with: CAPTCHANO_API_KEY=cn-... go run ./example
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	captchano "github.com/DishantSinghDev2/captchano-go"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client := captchano.New() // reads CAPTCHANO_API_KEY

	bal, err := client.Balance(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("balance: %s %s\n", bal.Balance, bal.Currency)

	task, err := client.RecaptchaV2(ctx, captchano.RecaptchaV2Params{
		URL:     "https://www.google.com/recaptcha/api2/demo",
		Sitekey: "6Le-wvkSAAAAAPBMRTvw0Q4Muexq9bi0DJwx_mJ-",
	})
	switch {
	case errors.Is(err, captchano.ErrInsufficientBalance):
		log.Fatal("top up at https://captchano.com/dashboard/topup")
	case err != nil:
		log.Fatal(err)
	}
	fmt.Printf("task %s ready, cost $%s\ntoken: %.40s...\n", task.ID, task.Cost, task.Token())
}
