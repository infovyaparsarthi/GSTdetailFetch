FROM python:3.11-slim

WORKDIR /app

# Install dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy application files
COPY . .

# Expose web server port
EXPOSE 4192

# Start Uvicorn web server
CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "4192"]
