module github.com/rashmirrout/DashCenter/src/impl-go/console

go 1.22

require (
	github.com/go-chi/chi/v5 v5.1.0
	github.com/go-chi/cors v1.2.1
	golang.org/x/time v0.5.0
)

replace github.com/rashmirrout/DashCenter/src/impl-go/gen/go => ../gen/go
