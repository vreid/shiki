module github.com/vreid/shiki/services/storage-worker

go 1.25.1

require (
	github.com/go-resty/resty/v2 v2.16.5
	github.com/urfave/cli/v3 v3.5.0
	github.com/valkey-io/valkey-go v1.0.67
	github.com/vreid/shiki/libs/go/types v0.0.0-00010101000000-000000000000
)

require (
	golang.org/x/net v0.38.0 // indirect
	golang.org/x/sys v0.31.0 // indirect
)

replace github.com/vreid/shiki/libs/go/types => ../../libs/go/types
