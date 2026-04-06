.PHONY: test test-python test-go test-ts up down build logs health

# Run all tests
test: test-python test-go test-ts

# Run Python shortener tests
test-python:
	cd services/shortener && pip install -q -r requirements.txt && python -m pytest test_main.py -v

# Run Go analytics tests
test-go:
	cd services/analytics && go test -v ./...

# Run TypeScript gateway tests
test-ts:
	cd services/gateway && npm install --silent && npm test

# Start all services
up:
	docker compose up -d --build

# Stop all services
down:
	docker compose down

# Build all images
build:
	docker compose build

# View logs
logs:
	docker compose logs -f

# Check health of all services
health:
	@echo "Shortener:" && curl -sf http://localhost:8001/health && echo
	@echo "Analytics:" && curl -sf http://localhost:8002/health && echo
	@echo "Gateway:"   && curl -sf http://localhost:8003/health && echo
