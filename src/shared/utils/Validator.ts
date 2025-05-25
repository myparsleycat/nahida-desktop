export default class Validator {
  static email(email: string) {
    const emailRegex = /^[a-zA-Z0-9._-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,6}$/;
    return emailRegex.test(email);
  }

  static checkLength(text: string, min: number, max: number) {
    const length = text.length;
    return length >= min && length <= max;
  }

  static url(url: string) {
    const urlRegex = /^(https?:\/\/)?([\da-z.-]+)\.([a-z.]{2,6})([/\w .-]*)*\/?$/;
    return urlRegex.test(url);
  }

  static phone(phone: string) {
    const numberOnly = phone.replace(/-/g, '');
    const mobilePattern = /^01([0|1|6|7|8|9])(\d{3,4})(\d{4})$/;
    const telPattern = /^(0[2-6][1-5])(\d{3,4})(\d{4})$/;
    return mobilePattern.test(numberOnly) || telPattern.test(numberOnly);
  }

  static ipv4(ip: string) {
    const ipv4Regex = /^(\d{1,3}\.){3}\d{1,3}$/;
    if (!ipv4Regex.test(ip)) return false;

    const numbers = ip.split('.').map(num => parseInt(num, 10));
    return numbers.every(num => num >= 0 && num <= 255);
  }

  static ipv6(ip: string) {
    const ipv6Regex = /^(?:[A-F0-9]{1,4}:){7}[A-F0-9]{1,4}$/i;

    const ipv6RegexCompressed = /^((?:[A-F0-9]{1,4}(?::[A-F0-9]{1,4})*)?)::((?:[A-F0-9]{1,4}(?::[A-F0-9]{1,4})*)?)$/i;

    return ipv6Regex.test(ip) || ipv6RegexCompressed.test(ip);
  }

  static ip(ip: string) {
    return this.ipv4(ip) || this.ipv6(ip);
  }

  static uuid(uuid: string) {
    const uuidRegex = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
    return uuidRegex.test(uuid);
  }

  static filePath(path: string, options?: { allowRelative?: boolean; maxLength?: number }) {
    const { allowRelative = true, maxLength = 260 } = options || {};
    
    if (!path || path.length === 0) return false;
    if (path.length > maxLength) return false;
    
    let pathToCheck = path;
    const isDrivePathMatch = path.match(/^([a-zA-Z]:)([\\/].*)$/);
    if (isDrivePathMatch) {
      pathToCheck = isDrivePathMatch[2];
    }
    
    const dangerousChars = /[<>"|?*\x00-\x1f]/;
    if (dangerousChars.test(pathToCheck)) return false;
    
    if (pathToCheck.includes(':')) return false;
    
    const isAbsolute = /^([a-zA-Z]:[\\/]|[\\/])/.test(path);
    if (!allowRelative && !isAbsolute) return false;
    
    if (/[\/\\]{2,}/.test(path)) return false;
    
    const reservedNames = /^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(\.|$)/i;
    const segments = path.split(/[\/\\]/);
    
    for (const segment of segments) {
      if (!segment) continue;
      if (reservedNames.test(segment)) return false;
      const nameWithoutExt = segment.includes('.') ? segment.substring(0, segment.lastIndexOf('.')) : segment;
      if (nameWithoutExt.endsWith(' ')) return false;
      if (!segment.includes('.') && segment.endsWith('.')) return false;
    }
    
    return true;
  }

  static windowsPath(path: string) {
    if (!path) return false;
    
    const windowsPathRegex = /^([a-zA-Z]:[\\/]|[\\/]{2}[^\\/]+[\\/][^\\/]+)/;
    return windowsPathRegex.test(path) && this.filePath(path, { allowRelative: false });
  }
  
  static unixPath(path: string) {
    if (!path) return false;
    
    const unixPathRegex = /^[\\/]/;
    return unixPathRegex.test(path) && this.filePath(path, { allowRelative: false });
  }
  
  static relativePath(path: string) {
    if (!path) return false;
    
    const isAbsolute = /^([a-zA-Z]:[\\/]|[\\/])/.test(path);
    return !isAbsolute && this.filePath(path, { allowRelative: true });
  }
  
  static fileName(name: string) {
    if (!name) return false;
    
    if (/[\/\\]/.test(name)) return false;
    
    const dangerousChars = /[<>:"|?*\x00-\x1f]/;
    if (dangerousChars.test(name)) return false;
    
    const reservedNames = /^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(\.|$)/i;
    if (reservedNames.test(name)) return false;
    
    if (name.endsWith('.') || name.endsWith(' ')) return false;
    
    if (name.length > 255) return false;
    
    return true;
  }
  
  static fileExtension(filename: string, allowedExtensions?: string[]) {
    if (!filename) return false;
    
    const extension = filename.split('.').pop()?.toLowerCase();
    if (!extension) return false;
    
    if (allowedExtensions) {
      const normalizedExtensions = allowedExtensions.map(ext => 
        ext.startsWith('.') ? ext.slice(1).toLowerCase() : ext.toLowerCase()
      );
      return normalizedExtensions.includes(extension);
    }
    
    return /^[a-z0-9]{1,10}$/i.test(extension);
  }
}