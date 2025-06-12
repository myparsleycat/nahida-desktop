import fs from 'fs-extra';
import path from 'node:path';
import Seven from 'node-7z';
import AdmZip from 'adm-zip';
import unrar from 'node-unrar-js';
import iconv from 'iconv-lite';
import { FSService } from '@core/services';

export async function extractFile({
  filePath, outputPath, delAfter
}: {
  filePath: string; outputPath?: string; delAfter: boolean;
}) {
  try {
    if (!fs.existsSync(filePath)) {
      throw new Error(`파일이 존재하지 않음: ${filePath}`);
    }

    const fileExt = path.extname(filePath).toLowerCase();
    const fileName = path.basename(filePath, fileExt);

    if (!outputPath) {
      const baseDir = path.dirname(filePath);
      outputPath = await FSService.generateUniqueFileName(baseDir, fileName);
    } else {
      outputPath = await FSService.generateUniqueFileName(path.dirname(outputPath), path.basename(outputPath));
    }

    fs.ensureDirSync(outputPath);

    let extractedPath = outputPath;
    switch (fileExt) {
      case '.zip':
        extractedPath = await extractZip(filePath, outputPath);
        break;
      case '.rar':
        extractedPath = await extractRar(filePath, outputPath);
        break;
      case '.7z':
        extractedPath = await extract7z(filePath, outputPath);
        break;
      default:
        throw new Error(`지원하지 않는 파일 형식: ${fileExt}`);
    }

    if (delAfter) {
      fs.removeSync(filePath);
    }

    return extractedPath;
  } catch (error: any) {
    console.error(`압축 해제 중 오류 발생: ${error.message}`);
    throw error;
  }
}

async function processNestedArchives(dirPath: string): Promise<void> {
  try {
    const files = fs.readdirSync(dirPath);

    for (const file of files) {
      const filePath = path.join(dirPath, file);
      const stats = fs.statSync(filePath);

      if (stats.isDirectory()) {
        await processNestedArchives(filePath);
      } else {
        const fileExt = path.extname(file).toLowerCase();
        if (['.zip', '.rar', '.7z'].includes(fileExt)) {
          const extractDirName = file.slice(0, file.lastIndexOf('.'));
          const extractDirPath = await FSService.generateUniqueFileName(dirPath, extractDirName);

          fs.ensureDirSync(extractDirPath);

          switch (fileExt) {
            case '.zip':
              await extractZip(filePath, extractDirPath);
              break;
            case '.rar':
              await extractRar(filePath, extractDirPath);
              break;
            case '.7z':
              await extract7z(filePath, extractDirPath);
              break;
          }

          fs.removeSync(filePath);
        }
      }
    }
  } catch (error: any) {
    throw new Error(`중첩된 압축 파일 처리 실패: ${error.message}`);
  }
}

function analyzeSingleRootFolder(
  entries: { path: string; isDirectory: boolean }[]
): { hasSingleRootFolder: boolean; rootFolder?: string } {
  const rootFolders = new Set<string>();
  let totalEntries = 0;

  for (const entry of entries) {
    if (!entry.isDirectory) {
      totalEntries++;
      const pathParts = entry.path.split(/[\/\\]/);
      if (pathParts.length > 1) {
        rootFolders.add(pathParts[0]);
      }
    }
  }

  const hasSingleRootFolder = rootFolders.size === 1 && totalEntries > 0;

  return {
    hasSingleRootFolder,
    rootFolder: hasSingleRootFolder ? Array.from(rootFolders)[0] : undefined
  };
}

async function removeNestedSingleFolders(dirPath: string): Promise<void> {
  try {
    const items = fs.readdirSync(dirPath);

    if (items.length === 1) {
      const singleItem = items[0];
      const singleItemPath = path.join(dirPath, singleItem);
      const stats = fs.statSync(singleItemPath);

      if (stats.isDirectory()) {
        const subItems = fs.readdirSync(singleItemPath);

        for (const subItem of subItems) {
          const srcPath = path.join(singleItemPath, subItem);
          const destPath = path.join(dirPath, subItem);

          let uniqueDestPath = destPath;
          try {
            await fs.access(destPath);
            uniqueDestPath = await FSService.generateUniqueFileName(dirPath, subItem);
          } catch { }

          fs.moveSync(srcPath, uniqueDestPath, { overwrite: false });
        }

        fs.removeSync(singleItemPath);
        await removeNestedSingleFolders(dirPath);
        return;
      }
    }

    // Process subdirectories recursively
    for (const item of items) {
      const itemPath = path.join(dirPath, item);
      const stats = fs.statSync(itemPath);

      if (stats.isDirectory()) {
        await removeNestedSingleFolders(itemPath);
      }
    }
  } catch (error: any) {
    throw new Error(`중첩된 싱글 폴더 제거 실패: ${error.message}`);
  }
}

async function handleSingleRootFolder(
  tempDir: string,
  rootFolder: string,
  outputPath: string
): Promise<void> {
  const rootFolderPath = path.join(tempDir, rootFolder);

  if (fs.existsSync(rootFolderPath)) {
    const files = fs.readdirSync(rootFolderPath);
    for (const file of files) {
      const srcPath = path.join(rootFolderPath, file);
      const destPath = path.join(outputPath, file);

      let uniqueDestPath = destPath;
      try {
        await fs.access(destPath);
        uniqueDestPath = await FSService.generateUniqueFileName(outputPath, file);
      } catch { }

      fs.moveSync(srcPath, uniqueDestPath, { overwrite: false });
    }
  }

  fs.removeSync(tempDir);
}

async function extractZip(filePath: string, outputPath: string): Promise<string> {
  try {
    const zip = new AdmZip(filePath);

    // Handle Chinese encoding issues
    zip.getEntries().forEach((entry) => {
      if (Buffer.isBuffer(entry.rawEntryName)) {
        try {
          let decodedName: string;
          decodedName = iconv.decode(entry.rawEntryName, 'cp936');

          entry.entryName = decodedName;
        } catch (e) {

          try {
            const decodedName = iconv.decode(entry.rawEntryName, 'utf-8');
            entry.entryName = decodedName;
          } catch (e2) { }
        }
      }
    });

    const zipEntries = zip.getEntries();

    const entries = zipEntries.map(entry => ({
      path: entry.entryName,
      isDirectory: entry.isDirectory
    }));

    const { hasSingleRootFolder, rootFolder } = analyzeSingleRootFolder(entries);

    if (hasSingleRootFolder && rootFolder) {
      const tempDir = path.join(outputPath, '_temp_extract');
      fs.ensureDirSync(tempDir);
      zip.extractAllTo(tempDir, true);

      await handleSingleRootFolder(tempDir, rootFolder, outputPath);
    } else {
      zip.extractAllTo(outputPath, true);
    }

    await removeNestedSingleFolders(outputPath);

    await processNestedArchives(outputPath);

    return outputPath;
  } catch (error: any) {
    throw new Error(`ZIP 파일 압축 해제 실패: ${error.message}`);
  }
}

async function extractRar(filePath: string, outputPath: string): Promise<string> {
  try {
    const listExtractor = await unrar.createExtractorFromFile({
      filepath: filePath
    });

    const list = listExtractor.getFileList();
    const fileHeaders = [...list.fileHeaders];

    const entries = fileHeaders.map(header => ({
      path: header.name,
      isDirectory: header.flags.directory
    }));

    const { hasSingleRootFolder, rootFolder } = analyzeSingleRootFolder(entries);

    if (hasSingleRootFolder && rootFolder) {
      const tempDir = path.join(outputPath, '_temp_extract');
      fs.ensureDirSync(tempDir);

      const extractor = await unrar.createExtractorFromFile({
        filepath: filePath,
        targetPath: tempDir
      });

      const extracted = extractor.extract();
      const files = [...extracted.files];

      await handleSingleRootFolder(tempDir, rootFolder, outputPath);
    } else {
      const extractor = await unrar.createExtractorFromFile({
        filepath: filePath,
        targetPath: outputPath
      });

      const extracted = extractor.extract();
      const files = [...extracted.files];
    }

    await removeNestedSingleFolders(outputPath);

    await processNestedArchives(outputPath);

    return outputPath;
  } catch (error: any) {
    throw new Error(`RAR 파일 압축 해제 실패: ${error.message}`);
  }
}

async function extract7z(filePath: string, outputPath: string): Promise<string> {
  return new Promise<string>((resolve, reject) => {
    try {
      const listStream = Seven.list(filePath);
      const entries: { path: string; isDirectory: boolean }[] = [];

      listStream.on('data', (entry) => {
        entries.push({
          path: entry.file,
          isDirectory: entry.attributes?.includes('D') || false
        });
      });

      listStream.on('end', async () => {
        const { hasSingleRootFolder, rootFolder } = analyzeSingleRootFolder(entries);

        if (hasSingleRootFolder && rootFolder) {
          const tempDir = path.join(outputPath, '_temp_extract');
          fs.ensureDirSync(tempDir);

          const extractStream = Seven.extractFull(filePath, tempDir, {
            $progress: true
          });

          extractStream.on('end', async () => {
            try {
              await handleSingleRootFolder(tempDir, rootFolder, outputPath);

              await removeNestedSingleFolders(outputPath);

              await processNestedArchives(outputPath);

              resolve(outputPath);
            } catch (error: any) {
              reject(new Error(`7Z 파일 단일 루트 폴더 처리 실패: ${error.message}`));
            }
          });

          extractStream.on('error', (err) => {
            reject(new Error(`7Z 파일 압축 해제 실패: ${err.message}`));
          });
        } else {
          const extractStream = Seven.extractFull(filePath, outputPath, {
            $progress: true
          });

          let fileCount = 0;

          extractStream.on('data', (data) => {
            if (data.status === 'extracted') {
              fileCount++;
            }
          });

          extractStream.on('end', async () => {
            await removeNestedSingleFolders(outputPath);

            await processNestedArchives(outputPath);
            resolve(outputPath);
          });

          extractStream.on('error', (err) => {
            reject(new Error(`7Z 파일 압축 해제 실패: ${err.message}`));
          });
        }
      });

      listStream.on('error', (err) => {
        reject(new Error(`7Z 파일 목록 가져오기 실패: ${err.message}`));
      });
    } catch (error: any) {
      reject(new Error(`7Z 파일 압축 해제 실패: ${error.message}`));
    }
  });
}