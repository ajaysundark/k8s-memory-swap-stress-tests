load("//corp/cloud/image_from_binary:build_defs.bzl", "image_from_binary")
load("//third_party/bazel_rules/rules_docker/container:container.bzl", "container_push")
load("//tools/build_defs/go:go_binary.bzl", "go_binary")

go_binary(
    name = "swap_stress_test",
    srcs = ["main.go"],
)

image_from_binary(
    name = "simple_swap_test",
    binary = ":swap_stress_test",
)

# Push it somewhere!
# (To use: "blaze run :simple_swap_test_pusher")
container_push(
    name = "simple_swap_test_pusher",
    format = "Docker",
    image = ":simple_swap_test",
    registry = "gcr.io",
    repository = "ajaysundar-gke-multi-cloud-dev/simple_swap_test",
)
