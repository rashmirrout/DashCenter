module github.com/rashmirrout/DashCenter/src/impl-go/console

go 1.22

require (
	github.com/coder/websocket v1.8.12
	github.com/go-chi/chi/v5 v5.1.0
	github.com/go-chi/cors v1.2.1
	github.com/prometheus/client_golang v1.19.1
	github.com/rashmirrout/DashCenter/src/impl-go/gen/go v0.0.0-00010101000000-000000000000
	golang.org/x/time v0.5.0
	google.golang.org/grpc v1.65.0
	google.golang.org/protobuf v1.34.2
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_model v0.5.0 // indirect
	github.com/prometheus/common v0.48.0 // indirect
	github.com/prometheus/procfs v0.12.0 // indirect
	golang.org/x/net v0.26.0 // indirect
	golang.org/x/sys v0.22.0 // indirect
	golang.org/x/text v0.16.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240528184218-531527333157 // indirect
)

replace github.com/rashmirrout/DashCenter/src/impl-go/gen/go => ../gen/go
