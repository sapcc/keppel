.PHONY: test
test: parser.go
	go test -v .

parser.go: parser.y
	goyacc -v "" -o parser.go parser.y

