build version="dev":
    go build -ldflags "-X main.version={{version}}" -o $(go env GOPATH)/bin/hud .

test:
    go test -cover -race ./...

run *args:
    go run . {{args}}
