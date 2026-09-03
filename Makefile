.PHONY: run build test ip clean

run:
	go run .

build:
	go build -o taal .

test:
	go test -race ./...

ip:
	@ipconfig getifaddr en0 2>/dev/null \
		|| ipconfig getifaddr en1 2>/dev/null \
		|| hostname -I 2>/dev/null | awk '{print $$1}'

clean:
	rm -f taal
