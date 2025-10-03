#!/usr/bin/env python3
"""
Simple test script to generate files and test Docker + Redis integration
"""

import pandas as pd
import matplotlib.pyplot as plt
import json
import os

# Create some sample data
data = {
    'product': ['Widget A', 'Widget B', 'Widget C', 'Widget D'],
    'sales': [100, 150, 200, 175],
    'price': [10.50, 15.75, 20.00, 18.25]
}

df = pd.DataFrame(data)

# Generate CSV file
csv_path = '/app/output/sales_data.csv'
df.to_csv(csv_path, index=False)
print(f"✅ Generated CSV: {csv_path}")

# Generate JSON summary
summary = {
    'total_sales': df['sales'].sum(),
    'average_price': df['price'].mean(),
    'products': len(df)
}

json_path = '/app/output/summary.json'
with open(json_path, 'w') as f:
    json.dump(summary, f, indent=2)
print(f"✅ Generated JSON: {json_path}")

# Generate simple text report
report_path = '/app/output/report.txt'
with open(report_path, 'w') as f:
    f.write("Sales Analysis Report\n")
    f.write("====================\n\n")
    f.write(f"Total Sales: {summary['total_sales']}\n")
    f.write(f"Average Price: ${summary['average_price']:.2f}\n")
    f.write(f"Number of Products: {summary['products']}\n")
print(f"✅ Generated Report: {report_path}")

print("🎉 All files generated successfully!")
