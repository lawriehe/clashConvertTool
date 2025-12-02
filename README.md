# A converting tool

## build
go mod tidy

GOOS=linux GOARCH=amd64 go build -o build/tool ./src/pkg

## run
./tool
