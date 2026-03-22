import json
import os
import glob
import re

directory = '/home/stevef/dev/artificial_mind/n8n/'
for filename in glob.glob(os.path.join(directory, '*.json')):
    with open(filename, 'r') as f:
        data = json.load(f)
    
    modified = False

    # Traverse nodes
    for node in data.get('nodes', []):
        if 'credentials' in node:
            # We can either delete it or empty it. 
            del node['credentials']
            modified = True
            
        # Scrub hardcoded chatIds in parameters
        params = node.get('parameters', {})
        if 'chatId' in params:
            val = params['chatId']
            # If it's a hardcoded number or string format number
            if isinstance(val, str) and (re.match(r'^=?-?\d+$', val)):
                params['chatId'] = '=YOUR_CHAT_ID'
                modified = True
        
    # Overwrite if we changed anything
    if modified:
        with open(filename, 'w') as f:
            json.dump(data, f, indent=2)
        print(f"Scrubbed {filename}")
    else:
        print(f"No credentials found in {filename}")

