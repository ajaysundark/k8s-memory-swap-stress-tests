FROM golang:latest
WORKDIR /app
COPY go.mod ./
COPY go.sum ./
RUN go mod download
COPY main.go ./
RUN go build -o /app/swap_stress_test
ENV PATH="$PATH:/app"
ENTRYPOINT ["/app/swap_stress_test"]