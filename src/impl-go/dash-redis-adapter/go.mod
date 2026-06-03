module github.com/rashmirrout/DashCenter/src/impl-go/dash-redis-adapter

go 1.22

require (
	github.com/alicebob/miniredis/v2 v2.32.1
	github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime v0.0.0-00010101000000-000000000000
	github.com/rashmirrout/DashCenter/src/impl-go/gen/go v0.0.0-00010101000000-000000000000
	github.com/redis/go-redis/v9 v9.5.1
	google.golang.org/grpc v1.65.0
	google.golang.org/protobuf v1.34.2
)

require (
	github.com/alicebob/gopher-json v0.0.0-20200520072559-a9ecdc9d1d3a // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	golang.org/x/net v0.25.0 // indirect
	golang.org/x/sys v0.20.0 // indirect
	golang.org/x/text v0.15.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240528184218-531527333157 // indirect
)

replace github.com/rashmirrout/DashCenter/src/impl-go/gen/go => ../gen/go

replace github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime => ../dashapi-runtime
