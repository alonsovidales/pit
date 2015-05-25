NO_COLOR=\033[0m
OK_COLOR=\033[32;01m
ERROR_COLOR=\033[31;01m
WARN_COLOR=\033[33;01m
DEPS = $(go list -f '{{range .TestImports}}{{.}} {{end}}' ./... | sort | uniq)

deps:
	@echo "$(OK_COLOR)==> deps"
	@echo "$(OK_COLOR)==> Installing dependencies$(NO_COLOR)"
	@go get -d -v ./...
	@echo $(DEPS) | xargs -n1 go get -d

updatedeps:
	@echo "$(OK_COLOR)==> updatedeps"
	@echo "$(OK_COLOR)==> Updating all dependencies$(NO_COLOR)"
	@go get -d -v -u ./...
	@echo $(DEPS) | xargs -n1 go get -d -u

format:
	@echo "$(OK_COLOR)==> format"
	@echo "$(OK_COLOR)==> Formatting$(NO_COLOR)"
	go fmt ./...

test:
	@echo "$(OK_COLOR)==> test"
	@echo "$(OK_COLOR)==> Testing$(NO_COLOR)"
	@find * -maxdepth 0 -mindepth 0 -type d  -not -path "*.*" | awk '{print "./" $$0 "/..."}' | xargs go test

lint:
	@echo "$(OK_COLOR)==> lint"
	@echo "$(OK_COLOR)==> Linting$(NO_COLOR)"
	golint ./...

deb:
	@echo "$(OK_COLOR)==> Building DEB$(NO_COLOR)"
	rm -rf dist_package
	mkdir -p dist_package/usr/local/bin
	mkdir -p dist_package/var/log/pit
	GOOS=linux GOARCH=amd64 go build -o dist_package/usr/local/bin/pit bin/pit.go
	GOOS=linux GOARCH=amd64 go build -o dist_package/usr/local/bin/pit-cli bin/pit-cli.go
	rm -rf tmp
	mkdir tmp
	cp -a etc dist_package/

	cd dist_package; tar czvf ../tmp/data.tar.gz *

	cd debian; tar czvf ../tmp/control.tar.gz *
	echo 2.0 > tmp/debian-binary
	ar -r pit.deb tmp/debian-binary tmp/control.tar.gz tmp/data.tar.gz
	rm -rf tmp
	@echo "$(OK_COLOR)==> Package created: pit.deb$(NO_COLOR)"

static_deb:
	@echo "$(OK_COLOR)==> Building Static DEB$(NO_COLOR)"
	rm -rf dist_package
	mkdir -p dist_package/var/www
	rm -rf tmp
	mkdir tmp
	cp -a static/* dist_package/var/www/

	cd dist_package; tar czvf ../tmp/data.tar.gz *

	cd debian_static; tar czvf ../tmp/control.tar.gz *
	echo 2.0 > tmp/debian-binary
	ar -r pit_static.deb tmp/debian-binary tmp/control.tar.gz tmp/data.tar.gz
	rm -rf tmp
	@echo "$(OK_COLOR)==> Static Package created: pit_static.deb$(NO_COLOR)"

deploy_dev: deb
	@ for SERVER in $$PIT_DEV_SERVERS ; do \
		echo "Uploading code to server: $(OK_COLOR)$$SERVER$(NO_COLOR)"; \
		scp -l 2400 -i $$HOME/.ssh/id_rsa_dev_pit pit.deb ubuntu@$$SERVER:/tmp/pit.deb ; \
	done
	@ for SERVER in $$PIT_DEV_SERVERS ; do \
		echo "Deploying new code on server: $(OK_COLOR)$$SERVER$(NO_COLOR)"; \
		ssh -i $$HOME/.ssh/id_rsa_dev_pit ubuntu@$$SERVER "sudo dpkg -i /tmp/pit.deb" ; \
	done

deploy_pro: deb
	ssh-add $$HOME/.ssh/id_rsa_pro_pit
	@ for SERVER in $$PIT_PRO_SERVERS ; do \
		echo "Uploading code to server: $(OK_COLOR)$$SERVER$(NO_COLOR)"; \
		scp -l 2400 -i $$HOME/.ssh/id_rsa_pro_pit pit.deb root@$$SERVER:/tmp/pit.deb ; \
	done
	@ for SERVER in $$PIT_PRO_SERVERS ; do \
		echo "Deploying new code on server: $(OK_COLOR)$$SERVER$(NO_COLOR)"; \
		ssh -i $$HOME/.ssh/id_rsa_pro_pit root@$$SERVER "dpkg -i /tmp/pit.deb" ; \
	done

deploy_static_pro: static_deb
	@ for SERVER in $$PIT_PRO_SERVERS ; do \
		echo "Uploading code to server: $(OK_COLOR)$$SERVER$(NO_COLOR)"; \
		scp -l 2400 -i $$HOME/.ssh/id_rsa_pro_pit pit_static.deb root@$$SERVER:/tmp/pit_static.deb ; \
	done
	@ for SERVER in $$PIT_PRO_SERVERS ; do \
		echo "Deploying new code on server: $(OK_COLOR)$$SERVER$(NO_COLOR)"; \
		ssh -i $$HOME/.ssh/id_rsa_pro_pit root@$$SERVER "dpkg -i /tmp/pit_static.deb" ; \
	done
