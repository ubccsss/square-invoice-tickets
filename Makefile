build:
	cd static && vulcanize --abspath . --strip-comments --inline-scripts --inline-css app.html > index.html
	go build

deps:
	bower install
	sudo npm install -g vulcanize

clean:
	rm -f static/index.html

.PHONY: build clean
