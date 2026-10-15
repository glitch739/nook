# Text extraction + OCR for attachment search (ADR 0009).
#
# The official full image ships Tesseract without Hungarian, and only
# the "fast" models. Measured on a real 150 DPI scanned contract: the
# "best" models rendered at 400 DPI (tika-config.json) read 30 of 30
# check phrases right, fast at 300 DPI 25 -- including a whole missed
# line. Best costs about 20 s per scanned page; it runs in the
# background, one file at a time.
#
# Tika 4 ignores the per-request X-Tika-OCRLanguage header -- the
# language has to come from the server config. hun+eng: Hungarian alone
# misreads Latin identifiers ("srvdb01" -> "srvdbO1").
FROM apache/tika:4.1.0-full
USER root
RUN apt-get update \
 && apt-get install -y --no-install-recommends tesseract-ocr-hun \
 && rm -rf /var/lib/apt/lists/*
# tessdata_best, pinned to its 4.1.0 tag.
ADD https://github.com/tesseract-ocr/tessdata_best/raw/4.1.0/hun.traineddata /usr/share/tesseract-ocr/5/tessdata/hun.traineddata
ADD https://github.com/tesseract-ocr/tessdata_best/raw/4.1.0/eng.traineddata /usr/share/tesseract-ocr/5/tessdata/eng.traineddata
RUN chmod 644 /usr/share/tesseract-ocr/5/tessdata/*.traineddata
COPY tika-config.json /opt/tika-config.json
USER 35002
CMD ["-c", "/opt/tika-config.json"]
