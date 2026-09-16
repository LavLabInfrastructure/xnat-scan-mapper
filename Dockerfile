FROM python:3.11-slim

RUN pip install --no-cache-dir requests

COPY src/map_and_zip.py /opt/map_and_zip.py
RUN chmod +x /opt/map_and_zip.py

ENTRYPOINT ["python3", "/opt/map_and_zip.py"]
