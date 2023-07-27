.DEFAULT_GOAL := build
.PHONY : build

IMAGE_NAME:=wechat-assistant

build:
	docker build -f build/Dockerfile -t ${IMAGE_NAME} .
test:
	docker build -f build/Dockerfile -t ${IMAGE_NAME}:test .
