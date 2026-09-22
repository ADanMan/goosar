'use client';

import { useCallback } from 'react';
import { toast } from 'sonner';
import { api } from '@goosar/core/api';
import { useFileUpload } from '@goosar/core/hooks/use-file-upload';
import { useT } from '../i18n';

function useEditorUpload() {
  const { t } = useT('editor');
  const onError = useCallback(
    (error: Error, file: File) => {
      toast.error(t(($) => $.upload.failed, { filename: file.name, reason: error.message }));
    },
    [t],
  );
  return useFileUpload(api, onError);
}

export { useEditorUpload };
