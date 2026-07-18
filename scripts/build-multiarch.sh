#!/bin/bash
set -e

cd "$(dirname "$0")/.."

PLATFORMS=${PLATFORMS:-"linux/amd64,linux/arm64,linux/arm/v7"}
IMAGE=${IMAGE:-"kspeeder/kspeeder-lite"}
TAG=${TAG:-"latest"}

echo "Building multi-arch image: ${IMAGE}:${TAG} for ${PLATFORMS}"

docker run --rm --privileged multiarch/qemu-user-static --reset -p yes 2>/dev/null || true
docker buildx create --name kspeeder-builder --driver docker-container --use 2>/dev/null || true
docker buildx inspect --bootstrap

docker buildx build \
  --platform "${PLATFORMS}" \
  -t "${IMAGE}:${TAG}" \
  -f docker/Dockerfile.architecture \
  --push .
