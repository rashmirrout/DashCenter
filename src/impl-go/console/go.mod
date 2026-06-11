module github.com/rashmirrout/DashCenter/src/impl-go/console

go 1.22

require (
	github.com/go-chi/chi/v5 v5.1.0
	github.com/go-chi/cors v1.2.1
	github.com/gorilla/websocket v1.5.3
	github.com/rashmirrout/DashCenter/src/impl-go/gen/go v0.0.0
	golang.org/x/sync v0.7.0
	golang.org/x/time v0.5.0
	google.golang.org/grpc v1.65.0
	google.golang.org/protobuf v1.34.2
)

replace (
	github.com/rashmirrout/DashCenter/src/impl-go/gen/go => ../gen/go
)