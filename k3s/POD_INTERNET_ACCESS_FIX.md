# Fixing Pod Internet Access with High-Performance Network

## Problem
After setting up a high-performance network for Kubernetes nodes, pods (news-ingestor and wiki-bootstrapper) cannot reach the internet, even though the nodes themselves can.

## Root Cause
Diagnostics revealed multiple issues:
1. **Node-level problem**: rpi4-4 cannot reach the internet (100% packet loss), while rpi58 and rpi5b can
2. **Pod scheduling**: Pods with `performance-tier: utility` can be scheduled on any utility node, including rpi4-4 which lacks internet access
3. **DNS resolution failure**: Even on nodes with internet access, DNS queries were failing or timing out, preventing hostname resolution (IP addresses work, but hostnames don't)

The CNI (Container Network Interface) routes pod traffic through the node's network, so if the node can't reach the internet, the pods can't either. Additionally, CoreDNS may be slow or unreliable, requiring fallback DNS servers.

## Solutions

### Solution 1: Node Affinity (RECOMMENDED - Already Applied)
Added node affinity to prefer scheduling pods on nodes with internet access (rpi58, rpi5b):
- Pods will prefer rpi58 and rpi5b over rpi4-4
- This ensures pods land on nodes that can reach the internet

**Apply the changes:**
```bash
kubectl apply -f k3s/news-ingestor-cronjob.yaml
kubectl apply -f k3s/wiki-bootstrapper-cronjob.yaml
```

### Solution 2: Fix rpi4-4 Pod Routing (DIAGNOSED)
**Root Cause Found**: rpi4-4 has two default routes:
- Primary: `eth0` via 192.168.10.1 (high-performance network, metric 100)
- Secondary: `wlan0` via 192.168.1.1 (WiFi, metric 600)

The CNI routes pod traffic through the primary route (eth0), but that network's gateway may not have proper internet access or NAT configuration.

**Diagnostic Results**:
- Node can reach internet (IPv6 works)
- Pods can reach their gateway (10.42.1.1)
- Pods cannot reach external IPs (packets get "host unreachable" at node level)
- Traceroute shows: `10.42.1.1 -> rpi4-4 (192.168.10.4) -> !H (host unreachable)`

**Fix Options**:

1. **Run diagnostic script on rpi4-4**:
   ```bash
   # Copy script to rpi4-4 and run:
   ./k3s/fix-rpi4-4-pod-routing.sh
   ```
   This will check NAT rules and suggest fixes.

2. **Ensure high-performance network gateway has internet**:
   - Verify 192.168.10.1 can reach internet
   - Check if NAT/masquerade is configured correctly

3. **Configure CNI to use WiFi for pod egress** (if high-performance network lacks internet):
   - Edit Flannel daemonset or CNI config
   - Or add route: `sudo ip route add 10.42.1.0/24 via 192.168.1.1 dev wlan0`

4. **Add NAT masquerade rule** (if missing):
   ```bash
   # On rpi4-4, find which interface has internet (wlan0 or eth0)
   # Then add masquerade:
   sudo iptables -t nat -A POSTROUTING -s 10.42.1.0/24 -o <interface-with-internet> -j MASQUERADE
   ```

### Solution 3: DNS Configuration (UPDATED - Already Applied)
Changed DNS policy to prioritize external DNS for reliable hostname resolution:
- `dnsPolicy: None` - Use custom DNS configuration
- **Primary nameservers**: Google DNS (8.8.8.8, 8.8.4.4) for fast external DNS resolution
- **Fallback**: CoreDNS (10.43.0.10) for cluster service resolution
- **Search domains**: Added cluster search domains so cluster services still resolve
- **DNS options**:
  - `ndots: 2` - Optimize DNS queries
  - `edns0` - Enable EDNS0 for larger DNS responses
  - `timeout: 1` - 1 second timeout per query (faster failover)
  - `attempts: 2` - Retry up to 2 times

This fixes the issue where pods can reach IP addresses (like 8.8.8.8) but cannot resolve hostnames (like bbc.co.uk). By prioritizing external DNS, hostname resolution should be fast and reliable.

**Note**: If DNS still fails, check if UDP port 53 is blocked by firewall rules on the nodes.

### Solution 4: Check CNI Routing Configuration
If you're using Flannel or another CNI, you may need to configure it to use the correct interface for egress.

**For Flannel (common in k3s):**
Check the Flannel configuration:
```bash
kubectl get configmap kube-flannel-cfg -n kube-system -o yaml
```

You may need to set the `iface` annotation on nodes to use the internet-capable interface:
```bash
kubectl annotate node <node-name> flannel.alpha.coreos.com/public-ip-overwrite=<node-ip>
```

### Solution 5: Configure Node Routing (Alternative)
Ensure nodes have proper routing for pod egress:

**On each node, check routing:**
```bash
ip route show
```

**Add route if needed (example for gateway 192.168.1.1):**
```bash
# This should already exist, but verify
ip route add default via 192.168.1.1 dev <internet-interface>
```

### Solution 6: Use Host Network (Not Recommended)
As a last resort, you can use `hostNetwork: true` in the pod spec. This makes pods use the node's network directly, but reduces security isolation.

**Warning:** Only use this if other solutions don't work, as it reduces pod network isolation.

### Solution 7: Configure CNI to Use Specific Interface
If using a custom CNI or k3s with Flannel, you can configure it to use the internet-capable interface:

**For k3s with Flannel:**
1. Edit the Flannel daemonset:
```bash
kubectl edit daemonset kube-flannel-ds -n kube-system
```

2. Add environment variable to use specific interface:
```yaml
env:
- name: KUBERNETES_NODENAME
  valueFrom:
    fieldRef:
      fieldPath: spec.nodeName
- name: FLANNELD_IFACE
  value: "<internet-interface-name>"  # e.g., eth0, wlan0
```

### Solution 6: Test Connectivity
Test if pods can now reach the internet:

```bash
# Create a test pod
kubectl run -it --rm test-pod --image=busybox --restart=Never -n agi -- sh

# Inside the pod, test connectivity:
ping -c 3 8.8.8.8
nslookup www.bbc.com
wget -O- https://www.bbc.com/news | head -20
```

## Diagnostic Commands

### Check pod network configuration:
```bash
# Get a pod name
POD=$(kubectl -n agi get pods -l job-name --field-selector status.phase=Running -o jsonpath='{.items[0].metadata.name}')

# Check pod network interfaces
kubectl -n agi exec $POD -- ip addr show

# Check pod routing
kubectl -n agi exec $POD -- ip route show

# Test DNS resolution
kubectl -n agi exec $POD -- nslookup www.bbc.com

# Test internet connectivity
kubectl -n agi exec $POD -- ping -c 3 8.8.8.8
```

### Check node network configuration:
```bash
# On each node, check interfaces
ip addr show

# Check routing table
ip route show

# Test internet from node
ping -c 3 8.8.8.8
```

## Recommended Approach

1. **First**: Apply the node affinity changes (Solution 1) - already done
   ```bash
   kubectl apply -f k3s/news-ingestor-cronjob.yaml
   kubectl apply -f k3s/wiki-bootstrapper-cronjob.yaml
   ```

2. **Second**: Fix rpi4-4 routing (Solution 2) if you want all nodes to have internet
   ```bash
   # SSH to rpi4-4 and run:
   ./k3s/fix-rpi4-4-routing.sh
   ```

3. **Third**: Test connectivity
   ```bash
   ./k3s/diagnose-pod-internet.sh
   ```

4. **If still failing**: Check CNI configuration (Solution 4 or 7)
5. **Last resort**: Use hostNetwork if absolutely necessary (Solution 6)

## Additional Notes

- The high-performance network is likely a separate interface (e.g., `eth1`, `enp1s0`) used for inter-node communication
- The internet-capable interface is likely the default interface (e.g., `eth0`, `wlan0`)
- k3s uses Flannel by default, which can be configured via annotations or environment variables
- If using a custom CNI, check its documentation for interface selection

