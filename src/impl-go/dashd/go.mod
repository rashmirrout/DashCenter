module github.com/rashmirrout/DashCenter/src/impl-go/dashd

go 1.22

require (
	github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime v0.0.0
	github.com/rashmirrout/DashCenter/src/impl-go/gen/go v0.0.0
	golang.org/x/time v0.5.0
	google.golang.org/protobuf v1.34.2
	gopkg.in/yaml.v3 v3.0.1
)

require (
	golang.org/x/net v0.26.0 // indirect
	golang.org/x/sys v0.21.0 // indirect
	golang.org/x/text v0.16.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240604185151-ef581f913117 // indirect
	google.golang.org/grpc v1.65.0 // indirect
)

replace (
	github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime => ../dashapi-runtime
	github.com/rashmirrout/DashCenter/src/impl-go/gen/go => ../gen/go
)
