IMAGE	 := keppel.eu-de-1.cloud.sap/ccloud/go-pmtud
DATE     := $(shell date +%Y%m%d%H%M%S)
VERSION  ?= v$(DATE)

.PHONY: all

all: build push

build:
	docker build -t $(IMAGE):$(VERSION) .

push: build
	docker push ${IMAGE}:${VERSION}
