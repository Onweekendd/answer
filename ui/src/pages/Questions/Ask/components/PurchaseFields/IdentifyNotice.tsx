/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

import { FC } from 'react';
import { Button, Spinner } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';

import type * as Type from '@/common/interface';

import type { IdentifyStatus } from './usePurchaseIdentify';

interface Props {
  status: IdentifyStatus;
  product: Type.PurchaseProduct | null;
  stale: boolean;
  manual: boolean;
  hasImages: boolean;
  /** which fields the reading filled in, for the "已填入…" note */
  filled: { price: boolean; tags: boolean };
  onIdentify: () => void;
  onDismiss: () => void;
}

/**
 * One line under the screenshots saying what identify is doing or found. It
 * never blocks the form: a failure or an empty reading is a quiet hint.
 */
const IdentifyNotice: FC<Props> = ({
  status,
  product,
  stale,
  manual,
  hasImages,
  filled,
  onIdentify,
  onDismiss,
}) => {
  const { t } = useTranslation('translation', {
    keyPrefix: 'ask.form.fields.images.identify',
  });

  const linkButton = (label: string, onClick: () => void) => (
    <Button
      variant="link"
      size="sm"
      className="p-0 align-baseline"
      onClick={onClick}>
      {label}
    </Button>
  );

  let filledNote = '';
  if (filled.price && filled.tags) {
    filledNote = t('filled_both');
  } else if (filled.price) {
    filledNote = t('filled_price');
  } else if (filled.tags) {
    filledNote = t('filled_tags');
  }

  let content: React.ReactNode = null;
  if (status === 'loading') {
    content = (
      <>
        <Spinner animation="border" size="sm" className="me-2" />
        {t('loading')}
      </>
    );
  } else if (product) {
    content = (
      <>
        <span className="me-2">
          ✨{' '}
          {filledNote
            ? t('result_filled', { name: product.name, filled: filledNote })
            : t('result', { name: product.name })}
        </span>
        {stale && hasImages && (
          <>
            <span className="text-warning me-2">{t('stale')}</span>
            <span className="me-2">{linkButton(t('retry'), onIdentify)}</span>
          </>
        )}
        {linkButton(t('dismiss'), onDismiss)}
      </>
    );
  } else if (status === 'error') {
    content = (
      <>
        <span className="me-2">{t('failed')}</span>
        {linkButton(t('retry'), onIdentify)}
      </>
    );
  } else if (status === 'done') {
    content = (
      <>
        <span className="me-2">{t('none')}</span>
        {linkButton(t('retry'), onIdentify)}
      </>
    );
  } else if (manual && hasImages) {
    content = linkButton(`✨ ${t('start')}`, onIdentify);
  }

  if (!content) {
    return null;
  }
  return (
    <div className="small text-secondary mt-2 d-flex align-items-center flex-wrap">
      {content}
    </div>
  );
};

export default IdentifyNotice;
