// Standard format: Return {"results": [...]} for all n8n webhooks
const allItems = $input.all();

if (!allItems || allItems.length === 0) {
  return [{ json: { results: [] } }];
}

const emails = [];

function isValidEmailObject(obj) {
  if (!obj || typeof obj !== 'object' || Array.isArray(obj)) {
    return false;
  }
  if (obj === null || obj === undefined) {
    return false;
  }
  return true;
}

function addEmail(emailObj) {
  if (isValidEmailObject(emailObj)) {
    const cleanEmail = {};
    for (const key in emailObj) {
      if (emailObj.hasOwnProperty(key)) {
        const value = emailObj[key];
        if (value !== null && value !== undefined) {
          cleanEmail[key] = value;
        }
      }
    }
    if (Object.keys(cleanEmail).length > 0) {
      emails.push(cleanEmail);
    }
  }
}

for (let i = 0; i < allItems.length; i++) {
  const item = allItems[i];
  
  if (!item || typeof item !== 'object' || !item.json) {
    continue;
  }
  
  const itemData = item.json;
  
  if (Array.isArray(itemData)) {
    for (const email of itemData) {
      addEmail(email);
    }
  } else if (isValidEmailObject(itemData)) {
    if (itemData.id || itemData.subject || itemData.Subject || itemData.from || itemData.From) {
      addEmail(itemData);
    } else if (itemData.json) {
      if (Array.isArray(itemData.json)) {
        for (const email of itemData.json) {
          addEmail(email);
        }
      } else if (isValidEmailObject(itemData.json)) {
        addEmail(itemData.json);
      }
    }
  }
}

// Return in standard format: {"results": [...]}
return [{ json: { results: emails } }];

