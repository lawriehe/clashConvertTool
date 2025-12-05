server: resource
	GOOS=linux GOARCH=amd64 go build -o build/tool ./src/pkg
resource:
	rm -rf build/configs
	rm -rf build/resources
	cp -r ./configs build/configs
	cp -r src/pkg/resources build/resources
clean:
	rm -rf build