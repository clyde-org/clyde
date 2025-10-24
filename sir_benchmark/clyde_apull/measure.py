import time
import csv

# CSV output file
csv_file = "timings.csv"
timing_data = []

def log_time(label, start_time, end_time):
    elapsed = end_time - start_time
    print(f"{label} took {elapsed:.2f} seconds")
    timing_data.append((label, round(elapsed, 2)))

# Step 1: Import libraries
start = time.time()
import torch
end = time.time()
log_time("Import torch", start, end)

start = time.time()
from transformers import AutoModelForCausalLM, AutoTokenizer, pipeline
end = time.time()
log_time("Import transformers", start, end)

start = time.time()
from flask import Flask, request, jsonify
end = time.time()
log_time("Import flask", start, end)

# Step 2: Load tokenizer
model_dir = "/app/Llama-3.2-1B"
start = time.time()
tokenizer = AutoTokenizer.from_pretrained(model_dir)
end = time.time()
log_time("Load Tokenizer", start, end)

# Step 3: Load model
start = time.time()
model = AutoModelForCausalLM.from_pretrained(model_dir)
end = time.time()
log_time("Load Model", start, end)

# Step 4: Create pipeline
start = time.time()
text_gen = pipeline(
    "text-generation",
    model=model,
    tokenizer=tokenizer,
    torch_dtype=torch.float16,
    device_map="auto",
)
end = time.time()
log_time("Create Pipeline", start, end)

# Step 5: Run inference
prompt = "Tell me a short history of The Gambia"
max_length = 200
start = time.time()
sequences = text_gen(
    prompt,
    do_sample=True,
    top_k=5,
    num_return_sequences=1,
    eos_token_id=tokenizer.eos_token_id,
    max_length=max_length,
)
end = time.time()
log_time("Run Inference", start, end)

# Output result
print(sequences)

# Step 6: Write timing data to CSV
with open(csv_file, mode='w', newline='') as f:
    writer = csv.writer(f)
    writer.writerow(["Operation", "Time (s)"])
    writer.writerows(timing_data)

print(f"Timing data saved to {csv_file}")
# export http_proxy=http://7.151.7.216:3128
# export https_proxy=http://7.151.7.216:3128


