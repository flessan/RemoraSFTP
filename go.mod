module remorasftp

go 1.22

require (
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.3
	github.com/pkg/sftp v1.13.9
	github.com/zalando/go-keyring v0.2.6
	golang.org/x/crypto v0.31.0
	golang.org/x/term v0.25.0
)

require (
	al.essio.dev/pkg/shellescape v1.5.1 // indirect
	github.com/danieljoos/wincred v1.2.2 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/kr/fs v0.1.0 // indirect
	golang.org/x/sys v0.28.0 // indirect
)

replace golang.org/x/crypto => github.com/golang/crypto v0.26.0

replace golang.org/x/sys => github.com/golang/sys v0.26.0

replace golang.org/x/term => github.com/golang/term v0.25.0

replace golang.org/x/net => github.com/golang/net v0.28.0

replace golang.org/x/text => github.com/golang/text v0.17.0

replace golang.org/x/sync => github.com/golang/sync v0.8.0

replace gopkg.in/yaml.v3 => ./thirdparty/yaml

replace golang.org/x/tools => ./thirdparty/tools

replace golang.org/x/mod => ./thirdparty/mod

replace golang.org/x/telemetry => ./thirdparty/telemetry

replace al.essio.dev/pkg/shellescape => ./thirdparty/shellescape
