import { Extension } from '@tiptap/core';
import { Plugin, PluginKey, TextSelection } from '@tiptap/pm/state';
import type { UploadResult } from '@goosar/core/hooks/use-file-upload';
import { createSafeId } from '@goosar/core/utils';

function findUploadNode(editor: any, uploadId: string): { pos: number; node: any } | null {
  let found: { pos: number; node: any } | null = null;
  editor.state.doc.descendants((node: any, pos: number) => {
    if (found) return false;
    if (
      (node.type.name === 'fileCard' || node.type.name === 'image') &&
      node.attrs.uploadId === uploadId
    ) {
      found = { pos, node };
      return false;
    }
    return undefined;
  });
  return found;
}

export function removeUploadNode(editor: any, uploadId: string): boolean {
  const hit = findUploadNode(editor, uploadId);
  if (!hit) return false;
  editor.view.dispatch(editor.state.tr.delete(hit.pos, hit.pos + hit.node.nodeSize));
  return true;
}

export function settleUploadNode(editor: any, uploadId: string, result: UploadResult): boolean {
  const hit = findUploadNode(editor, uploadId);
  if (!hit) return false;
  const href = result.markdownLink || result.link;
  const isImage = (result.content_type ?? '').startsWith('image/');
  const tr = editor.state.tr;

  if (hit.node.type.name === 'image') {
    tr.setNodeMarkup(hit.pos, undefined, {
      ...hit.node.attrs,
      src: href,
      alt: result.filename,
      uploading: false,
      uploadId: null,
    });
  } else if (isImage) {
    tr.replaceWith(
      hit.pos,
      hit.pos + hit.node.nodeSize,
      editor.schema.nodes.image.create({ src: href, alt: result.filename, uploading: false }),
    );
  } else {
    tr.setNodeMarkup(hit.pos, undefined, {
      ...hit.node.attrs,
      href,
      uploading: false,
      uploadId: null,
    });
  }
  editor.view.dispatch(tr);
  return true;
}

export function insertUploadPlaceholder(
  editor: any,
  upload: { uploadId: string; filename: string; size?: number },
): boolean {
  if (findUploadNode(editor, upload.uploadId)) return true;
  const endPos = editor.state.doc.content.size;
  editor
    .chain()
    .insertContentAt(endPos, {
      type: 'fileCard',
      attrs: {
        filename: upload.filename,
        href: '',
        fileSize: upload.size ?? 0,
        uploading: true,
        uploadId: upload.uploadId,
      },
    })
    .run();
  return true;
}

export function findImagePosBySrc(editor: any, src: string): number | null {
  if (!editor) return null;
  let imagePos: number | null = null;
  editor.state.doc.descendants((node: any, pos: number) => {
    if (imagePos !== null) return false;
    if (node.type.name === 'image' && node.attrs.src === src) {
      imagePos = pos;
      return false;
    }
    return undefined;
  });
  return imagePos;
}

async function readImageDimensions(file: File): Promise<{ width: number; height: number } | null> {
  if (typeof createImageBitmap !== 'function') return null;
  try {
    const bitmap = await createImageBitmap(file);
    const dims = { width: bitmap.width, height: bitmap.height };
    bitmap.close();
    return dims.width > 0 && dims.height > 0 ? dims : null;
  } catch {
    return null;
  }
}

async function applyImageDimensions(editor: any, file: File, src: string) {
  const dims = await readImageDimensions(file);
  if (!dims) return;

  const imagePos = findImagePosBySrc(editor, src);
  if (imagePos === null) return;

  const imageNode = editor.state.doc.nodeAt(imagePos);
  if (!imageNode || imageNode.attrs.width) return;

  const tr = editor.state.tr.setNodeMarkup(imagePos, undefined, {
    ...imageNode.attrs,
    width: dims.width,
    height: dims.height,
  });
  editor.view.dispatch(tr);
}

function moveSelectionToParagraphAfterImage(editor: any, src: string) {
  const imagePos = findImagePosBySrc(editor, src);
  if (imagePos === null) return;

  const imageNode = editor.state.doc.nodeAt(imagePos);
  if (!imageNode) return;

  const afterImagePos = imagePos + imageNode.nodeSize;
  const $afterImage = editor.state.doc.resolve(afterImagePos);
  if ($afterImage.nodeAfter?.type.name !== 'paragraph') return;

  const paragraphStart = afterImagePos + 1;
  const tr = editor.state.tr
    .setSelection(TextSelection.create(editor.state.doc, paragraphStart))
    .scrollIntoView();
  editor.view.dispatch(tr);
}

export async function uploadAndInsertFile(
  editor: any,
  file: File,
  handler: (file: File, uploadId: string) => Promise<UploadResult | null>,
  pos?: number,
) {
  const isImage = file.type.startsWith('image/');
  const uploadId = createSafeId();

  if (isImage) {
    const blobUrl = URL.createObjectURL(file);
    const imgAttrs = { src: blobUrl, alt: file.name, uploading: true, uploadId };
    if (pos !== undefined) {
      editor.chain().focus().insertContentAt(pos, { type: 'image', attrs: imgAttrs }).run();
    } else {
      editor.chain().focus().setImage(imgAttrs).run();
      moveSelectionToParagraphAfterImage(editor, blobUrl);
    }

    void applyImageDimensions(editor, file, blobUrl);

    try {
      const result = await handler(file, uploadId);
      if (editor.isDestroyed) return;
      if (result) settleUploadNode(editor, uploadId, result);
      else removeUploadNode(editor, uploadId);
    } catch {
      if (!editor.isDestroyed) removeUploadNode(editor, uploadId);
    } finally {
      URL.revokeObjectURL(blobUrl);
    }
  } else {
    const cardAttrs = {
      filename: file.name,
      href: '',
      fileSize: file.size,
      uploading: true,
      uploadId,
    };
    const insertContent = { type: 'fileCard', attrs: cardAttrs };
    if (pos !== undefined) {
      editor.chain().focus().insertContentAt(pos, insertContent).run();
    } else {
      editor.chain().focus().insertContent(insertContent).run();
    }

    try {
      const result = await handler(file, uploadId);
      if (editor.isDestroyed) return;
      if (result) settleUploadNode(editor, uploadId, result);
      else removeUploadNode(editor, uploadId);
    } catch {
      if (!editor.isDestroyed) removeUploadNode(editor, uploadId);
    }
  }
}

function dedupFiles(files: FileList): File[] {
  const seen = new Set<string>();
  return Array.from(files).filter((file) => {
    const key = `${file.name}\0${file.size}\0${file.type}`;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

export const PASTED_TEXT_FILENAME = 'pasted-text.txt';

const pastedTextSources = new WeakMap<File, string>();

export function markPastedTextFile(file: File, text: string): File {
  pastedTextSources.set(file, text);
  return file;
}

export function pastedTextSource(file: File): string | undefined {
  return pastedTextSources.get(file);
}

export function createFileUploadExtension(
  onUploadFileRef: React.RefObject<
    ((file: File, uploadId: string) => Promise<UploadResult | null>) | undefined
  >,
  pasteAsFileThresholdRef?: React.RefObject<number | undefined>,
) {
  return Extension.create({
    name: 'fileUpload',
    addProseMirrorPlugins() {
      const { editor } = this;

      const handleFiles = async (files: File[]) => {
        const handler = onUploadFileRef.current;
        if (!handler) return false;
        for (const file of files) {
          await uploadAndInsertFile(editor, file, handler);
        }
        return true;
      };

      return [
        new Plugin({
          key: new PluginKey('fileUpload'),
          props: {
            handlePaste(_view, event) {
              const files = event.clipboardData?.files;
              if (!files?.length) {
                const threshold = pasteAsFileThresholdRef?.current;
                if (!threshold || threshold <= 0) return false;
                const text = event.clipboardData?.getData('text/plain') ?? '';
                if (text.length <= threshold) return false;
                if (!onUploadFileRef.current) return false;
                if (editor.isActive('codeBlock')) return false;
                const file = new File([text], PASTED_TEXT_FILENAME, { type: 'text/plain' });
                handleFiles([markPastedTextFile(file, text)]);
                return true;
              }
              if (!onUploadFileRef.current) return false;
              handleFiles(dedupFiles(files));
              return true;
            },
            handleDrop(view, event) {
              const dragEvent = event as DragEvent;
              const files = dragEvent.dataTransfer?.files;
              if (!files?.length) return false;
              const handler = onUploadFileRef.current;
              if (!handler) return false;
              const dropPos = view.posAtCoords({ left: dragEvent.clientX, top: dragEvent.clientY });
              const unique = dedupFiles(files);
              for (let i = 0; i < unique.length; i++) {
                const insertPos = i === 0 ? dropPos?.pos : undefined;
                uploadAndInsertFile(editor, unique[i]!, handler, insertPos);
              }
              return true;
            },
          },
        }),
      ];
    },
  });
}
