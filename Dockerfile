FROM scratch

COPY dox /usr/bin/dox

ENTRYPOINT ["/usr/bin/dox"]
CMD ["--help"]
