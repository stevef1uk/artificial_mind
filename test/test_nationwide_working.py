import requests
from bs4 import BeautifulSoup
import json
import sys

# URL
url = "https://www.nationwide.co.uk/savings/compare-savings-accounts-and-isas/"

try:
    # Fetch content
    print(f"Fetching {url}...", file=sys.stderr)
    r = requests.get(url, headers={"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"})
    r.raise_for_status()
    
    # Parse HTML
    soup = BeautifulSoup(r.text, 'html.parser')
    
    # Find Next.js data
    script = soup.find('script', id='__NEXT_DATA__')
    if not script:
        print("Error: Could not find __NEXT_DATA__ script tag.", file=sys.stderr)
        sys.exit(1)
        
    # Load JSON
    data = json.loads(script.string)
    
    # Determine Extraction Method
    # Path: props -> pageProps -> additionalData -> SavingsRatesTable -> products
    
    products = []
    try:
        if 'props' in data and 'pageProps' in data['props']:
             pp = data['props']['pageProps']
             if 'additionalData' in pp and 'SavingsRatesTable' in pp['additionalData']:
                  products = pp['additionalData']['SavingsRatesTable']['products']
    except Exception as e:
        print(f"Traverse error: {e}", file=sys.stderr)

    if not products:
        # Fallback recursive search for 'products' key if exact path fails
        def find_products(d):
            if isinstance(d, dict):
                for k, v in d.items():
                    if k == 'products' and isinstance(v, list) and len(v) > 0 and 'name' in v[0]:
                        return v
                    if isinstance(v, (dict, list)):
                        found = find_products(v)
                        if found: return found
            elif isinstance(d, list):
                for item in d:
                    found = find_products(item)
                    if found: return found
            return None
        products = find_products(data)

    if not products:
        print("Error: Could not find 'products' key in JSON path.", file=sys.stderr)
        sys.exit(1)
        
    print(f"Found {len(products)} products.", file=sys.stderr)
    
    # Extract
    results = []
    for p in products:
        name = p.get('name', 'Unknown')
        # Rates logic
        issues = p.get('issues', [])
        rate_str = "N/A"
        
        # Try to find max AER across subproducts/tiers
        max_aer = 0.0
        found_aer = False
        
        for issue in issues:
            subproducts = issue.get('subproducts', [])
            for sub in subproducts:
                tiers = sub.get('rateTiers', [])
                for tier in tiers:
                    aer = tier.get('aer')
                    if aer is not None:
                        try:
                            val = float(aer)
                            if val > max_aer: 
                                max_aer = val
                                found_aer = True
                        except: pass
                        
        if found_aer:
            rate_str = f"{max_aer}% AER"
            
        results.append({"product": name, "rate": rate_str})
        
    # Print Clean Output
    print(json.dumps(results, indent=2))
        
except Exception as e:
    print(f"Exception: {e}", file=sys.stderr)
    sys.exit(1)
