package main

import (
	"log"

	"github.com/AbdullahZaher/ruxbase"
	"github.com/AbdullahZaher/ruxbase/core"
)

func main() {
	app := ruxbase.New()
	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		e.Router.GET("/docs-example/hello", func(re *core.RequestEvent) error {
			return re.JSON(200, map[string]string{"message": "hello"})
		})
		return e.Next()
	})
	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
