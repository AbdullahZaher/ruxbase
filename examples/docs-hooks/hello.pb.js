routerAdd("GET", "/docs-example/js-hello", (e) => {
    return e.json(200, { message: "hello from JS" });
});
