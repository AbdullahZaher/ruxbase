---
title: "Extend Ruxbase with JavaScript hooks"
description: "Register a server-side JavaScript route using the embedded Goja runtime."
---

**Development documentation.** Use disposable development data and ordinary record credentials for application clients. Verify the installed server version before using examples.

## Runtime
The default examples/base executable registers jsvm and migratecmd. Server-side JS runs in the embedded Goja runtime, not a browser or a complete Node.js runtime. The hooks directory defaults to pb_hooks beside the data directory; files use the .pb.js convention. The --hooksDir and --hooksWatch flags let you control loading and restarts.

## First custom route
Save this as hello.pb.js in the chosen hooks directory:
```js
routerAdd("GET", "/docs-example/js-hello", (e) => {
    return e.json(200, { message: "hello from JS" });
});
```
Start the normal Ruxbase binary with that directory:
```sh
./ruxbase serve --dev --hooksDir=./pb_hooks --hooksWatch=false
```
GET /docs-example/js-hello returns a constant public JSON response. No collections or secrets are exposed. The identical example file is committed under examples/docs-hooks/hello.pb.js and will be loaded by quickstart CI.

## Evidence
[Source: examples/base/main.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/examples/base/main.go), [Source: plugins/jsvm/jsvm.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/plugins/jsvm/jsvm.go), [Source: plugins/jsvm/binds.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/plugins/jsvm/binds.go). See [model and migration constraints](/ruxbase/extend-js/data-migrations/).
