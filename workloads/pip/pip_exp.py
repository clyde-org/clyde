#!/usr/bin/env python3
"""
AI package installation timer with size tracking
"""

import subprocess
import time
import csv
import argparse
import re
from datetime import datetime

# Largest AI packages including torch and tensorflow
PACKAGES = [
    "torch",
    "tensorflow",
    "torchvision",
    "torchaudio",
    "jax[cpu]",
    "transformers", 
    "mxnet",
    "opencv-python",
    "xgboost",
    "lightgbm",
    "pyspark",
]

def get_package_size(package_name):
    """Get package size from pip show output"""
    try:
        result = subprocess.run(
            ["pip", "show", package_name],
            capture_output=True,
            text=True,
            timeout=30
        )
        if result.returncode == 0:
            # Extract size from pip show output
            for line in result.stdout.split('\n'):
                if line.startswith('Size:'):
                    return line.split(':')[1].strip()
    except:
        pass
    return "N/A"

def install_packages():
    results = []
    
    for package in PACKAGES:
        # Build the command
        cmd = ["pip", "install", package]
        
        # if no_cache:
        #     cmd.append("--no-cache-dir")
        # if force_reinstall:
        #     cmd.append("--force-reinstall")
        
        print(f"Installing {package}...")
        start_time = time.time()
        
        try:
            result = subprocess.run(cmd, capture_output=True, text=True, timeout=1200)
            end_time = time.time()
            time_taken = round(end_time - start_time, 2)
            
            # Get package size after installation
            size = get_package_size(package.split('[')[0])  # Remove extras like [cpu]
            
            if result.returncode == 0:
                status = "SUCCESS"
                print(f"✓ {package} installed in {time_taken}s, Size: {size}")
            else:
                status = "FAILED"
                print(f"✗ {package} failed after {time_taken}s")
                if result.stderr:
                    print(f"Error: {result.stderr[:100]}...")
                
        except subprocess.TimeoutExpired:
            time_taken = 1200
            status = "TIMEOUT"
            size = "N/A"
            print(f"✗ {package} timed out after 20 minutes")
        except Exception as e:
            time_taken = round(time.time() - start_time, 2)
            status = "ERROR"
            size = "N/A"
            print(f"✗ {package} error: {str(e)}")
            
        results.append({
            "package": package,
            "status": status,
            "time_seconds": time_taken,
            "size": size,
            "timestamp": datetime.now().isoformat()
        })
    
    return results

def main():
    # parser = argparse.ArgumentParser(description="Install AI packages and track size/time")
    # parser.add_argument("--trusted-host", default="7.151.6.248", 
    #                    help="Trusted host for pip install")
    # parser.add_argument("--no-cache", action="store_true",
    #                    help="Disable pip cache")
    # parser.add_argument("--force-reinstall", action="store_true",
    #                    help="Force reinstall packages")
    
    # args = parser.parse_args()
    
    # print(f"Starting installation with trusted-host: {args.trusted_host}")
    # if args.no_cache:
    #     print("Cache: disabled")
    # if args.force_reinstall:
    #     print("Mode: force reinstall")
    
    results = install_packages()
    
    # Save to CSV
    csv_file = "package_install_stats.csv"
    with open(csv_file, 'w', newline='') as f:
        writer = csv.DictWriter(f, fieldnames=["package", "status", "time_seconds", "size", "timestamp"])
        writer.writeheader()
        writer.writerows(results)
    
    print(f"\nResults saved to {csv_file}")
    
    # Summary
    success_count = sum(1 for r in results if r["status"] == "SUCCESS")
    total_time = sum(r["time_seconds"] for r in results)
    
    print(f"\nSummary: {success_count}/{len(PACKAGES)} packages installed")
    print(f"Total time: {total_time}s")

if __name__ == "__main__":
    main()