---
title: "Extend Ruxbase with Go"
description: "Build a custom Go executable with an explicit route and preserve application lifecycle."
---

**Development documentation.** Use disposable development data and ordinary record credentials for application clients. Verify the installed server version before using examples.

## Go executable
Ruxbase exposes the Go framework in the same module as the server. Start in a checkout of the target version and use the Go toolchain from go.mod. The following minimal executable deliberately does not register the optional JS plugins; use examples/base to retain the full default CLI and VM setup.

```go
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
```
The identical source is at examples/docs-go/main.go. Build it from the repository root:
```sh
go build -o ruxbase-docs-go ./examples/docs-go
./ruxbase-docs-go serve --dev --http=127.0.0.1:8091
```
GET http://127.0.0.1:8091/docs-example/hello returns {"message":"hello"}. The route deliberately returns a constant public response without user data. The quickstart CI will compile and probe it on a disposable development server.

## Hooks and routing
Register hooks on app before Start. Use e.Next() to continue the hook chain; an error interrupts it. Protect routes that access application data; custom server routes do not automatically implement collection API rules.

## Evidence
[Source: examples/base/main.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/examples/base/main.go), [Source: core/app.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/core/app.go). See [data and hook patterns](/ruxbase/extend-go/data-hooks/).
