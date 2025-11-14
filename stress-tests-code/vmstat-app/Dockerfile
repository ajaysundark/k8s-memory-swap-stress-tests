# Start from a standard Ubuntu base image
FROM ubuntu:22.04

# Install the 'procps' package, which contains vmstat
RUN apt-get update && apt-get install -y procps && rm -rf /var/lib/apt/lists/*