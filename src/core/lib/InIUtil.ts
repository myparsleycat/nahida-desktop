// src/core/lib/iniParser.ts

export interface KeySwapConfig {
  key: string;
  back?: string;
  cycle: number;
  values?: string[];
  type?: string;
}

export interface IniParseResult {
  sectionName: string;
  key: string;
  back?: string;
  cycle: number;
  type: string;
  values: string[];
  varName: string;
}

class InIUtilClass {
  isKeySection(sectionName: string) {
    return sectionName.startsWith('Key') && sectionName !== 'Key';
  }

  parse(content: string) {
    const lines = content.split(/\r?\n/);
    let currentSection = '';
    const tempResult: Record<string, any> = {};
    const sectionVars: Record<string, Array<{ name: string, values: string[] }>> = {};

    for (let i = 0; i < lines.length; i++) {
      const line = lines[i].trim();

      if (line.startsWith(';') || line === '') {
        continue;
      }

      const sectionMatch = line.match(/^\[(.*)\]$/);
      if (sectionMatch) {
        currentSection = sectionMatch[1];

        if (this.isKeySection(currentSection)) {
          tempResult[currentSection] = {
            sectionName: currentSection
          };
          sectionVars[currentSection] = [];
        }
        continue;
      }

      if (!this.isKeySection(currentSection)) {
        continue;
      }

      const keyValueMatch = line.match(/^([^=]+)=(.*)$/);
      if (keyValueMatch) {
        const key = keyValueMatch[1].trim().toLowerCase();
        const value = keyValueMatch[2].trim();

        if (key === 'key') {
          tempResult[currentSection].key = value;
        } else if (key === 'back') {
          tempResult[currentSection].back = value;
        } else if (key === 'type' && value === 'cycle') {
          tempResult[currentSection].type = value;
        } else if (key.startsWith('$')) {
          // 모든 $ 변수를 저장
          const cycleValues = value.split(',').map(v => v.trim());
          sectionVars[currentSection].push({
            name: key,
            values: cycleValues
          });
        }
      }
    }

    // 각 섹션에서 가장 긴 값 목록을 가진 $ 변수 선택
    for (const section in sectionVars) {
      if (sectionVars[section].length > 0) {
        // 값 목록 길이 기준으로 내림차순 정렬
        sectionVars[section].sort((a, b) => b.values.length - a.values.length);

        const longestVar = sectionVars[section][0];
        tempResult[section].cycle = longestVar.values.length;
        tempResult[section].values = longestVar.values;
        tempResult[section].varName = longestVar.name;
      }
    }

    const resultArray = Object.values(tempResult).map(section => {
      const result: any = {
        sectionName: section.sectionName,
        key: section.key,
        cycle: section.cycle || 0,
        type: section.type
      };

      if (section.back !== undefined) {
        result.back = section.back;
      }

      if (section.values) {
        result.values = section.values;
        result.varName = section.varName;
      }

      return result;
    });

    return resultArray;
  }

  update(content: string, section: string, key: "key" | "back", newValue: string) {
    const lines = content.split(/\r?\n/);
    let currentSection = '';
    let inTargetSection = false;

    for (let i = 0; i < lines.length; i++) {
      const originalLine = lines[i];
      const line = originalLine.trim();

      const sectionMatch = line.match(/^\[(.*)\]$/);
      if (sectionMatch) {
        currentSection = sectionMatch[1];
        inTargetSection = (currentSection === section);
        continue;
      }

      if (inTargetSection) {
        const keyValueMatch = line.match(/^([^=]+)=(.*)$/);
        if (keyValueMatch && keyValueMatch[1].trim().toLowerCase() === key.toLowerCase()) {
          const indent = originalLine.match(/^(\s*)/)?.[1] || '';
          lines[i] = `${indent}${key} = ${newValue}`;
          break;
        }
      }
    }

    return lines.join('\n');
  }

  addKeySection(
    content: string,
    sectionName: string,
    config: KeySwapConfig
  ) {
    const lines = content.split(/\r?\n/);

    let lastKeyIndex = -1;
    for (let i = lines.length - 1; i >= 0; i--) {
      const line = lines[i].trim();
      const sectionMatch = line.match(/^\[(.*)\]$/);
      if (sectionMatch && this.isKeySection(sectionMatch[1])) {
        lastKeyIndex = i;
        break;
      }
    }

    const newSection = [
      `[${sectionName}]`,
      `key = ${config.key}`
    ];

    if (config.back !== undefined) {
      newSection.push(`back = ${config.back}`);
    }

    newSection.push(`type = cycle`);

    if (config.values && config.values.length > 0) {
      const varName = `$swapvar${config.key.toLowerCase()}`;
      newSection.push(`${varName} = ${config.values.join(',')}`);
    } else if (config.cycle > 0) {
      const varName = `$swapvar${config.key.toLowerCase()}`;
      const values = Array.from({ length: config.cycle }, (_, i) => i.toString());
      newSection.push(`${varName} = ${values.join(',')}`);
    }

    if (lastKeyIndex >= 0) {
      let insertIndex = lastKeyIndex;
      while (insertIndex < lines.length &&
        !lines[insertIndex].trim().match(/^\[.*\]$/) &&
        lines[insertIndex].trim() !== '') {
        insertIndex++;
      }
      lines.splice(insertIndex, 0, '', ...newSection);
    } else {
      lines.push('', ...newSection);
    }

    return lines.join('\n');
  }

  removeKeySection(content: string, sectionName: string) {
    const lines = content.split(/\r?\n/);
    const result: string[] = [];

    let inTargetSection = false;
    let i = 0;

    while (i < lines.length) {
      const line = lines[i].trim();

      const sectionMatch = line.match(/^\[(.*)\]$/);
      if (sectionMatch) {
        const currentSection = sectionMatch[1];

        if (currentSection === sectionName) {
          inTargetSection = true;
          i++;
          continue;
        } else {
          inTargetSection = false;
        }
      }

      if (!inTargetSection) {
        result.push(lines[i]);
      }

      i++;
    }

    return result.join('\n');
  }
}

const iniutil = new InIUtilClass();
export { iniutil };