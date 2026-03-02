.PHONY: build test run clean

build:
	docker-compose -f docker-compose.test.yml build

test:
	./run.sh

run:
	./run.sh

clean:
	docker-compose -f docker-compose.test.yml down -v --remove-orphans
