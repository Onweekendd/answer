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
import { Form, InputGroup } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';

import { Icon, UploadImg } from '@/components';
import type * as Type from '@/common/interface';

import './index.scss';

export const MAX_PURCHASE_IMAGES = 5;

interface Props {
  images: Type.FormValue<string[]>;
  price: Type.FormValue<string>;
  link: Type.FormValue<string>;
  onImagesChange: (images: string[]) => void;
  onPriceChange: (price: string) => void;
  onLinkChange: (link: string) => void;
}

/**
 * Purchase-specific fields of the post form: screenshots (1-5), price and product link.
 * Images are uploaded through the existing `POST /file` endpoint (`source=post`),
 * so the returned URLs already live under the site's upload directory.
 */
const PurchaseFields: FC<Props> = ({
  images,
  price,
  link,
  onImagesChange,
  onPriceChange,
  onLinkChange,
}) => {
  const { t } = useTranslation('translation', { keyPrefix: 'ask.form.fields' });

  const handleUploaded = (url: string) => {
    if (!url || images.value.length >= MAX_PURCHASE_IMAGES) {
      return;
    }
    onImagesChange([...images.value, url]);
  };

  const handleRemove = (index: number) => {
    onImagesChange(images.value.filter((_, i) => i !== index));
  };

  const canUploadMore = images.value.length < MAX_PURCHASE_IMAGES;

  return (
    <>
      <Form.Group controlId="images" className="mb-3">
        <Form.Label>{t('images.label')}</Form.Label>
        <div className="purchase-images d-flex flex-wrap gap-2">
          {images.value.map((url, index) => (
            <div
              key={url}
              className="purchase-image-item position-relative rounded border overflow-hidden">
              <img src={url} alt="" className="w-100 h-100 object-fit-cover" />
              {index === 0 && (
                <span className="badge bg-dark position-absolute top-0 start-0 m-1 opacity-75">
                  {t('images.cover')}
                </span>
              )}
              <button
                type="button"
                className="btn btn-sm btn-dark purchase-image-remove position-absolute top-0 end-0 m-1 p-0 opacity-75"
                aria-label="remove"
                onClick={() => handleRemove(index)}>
                <Icon name="x" />
              </button>
            </div>
          ))}
          {canUploadMore && (
            <UploadImg
              type="post"
              className="purchase-image-item purchase-image-add d-flex align-items-center justify-content-center rounded border border-dashed mb-0"
              uploadCallback={handleUploaded}>
              <Icon name="plus-lg" className="fs-4" />
            </UploadImg>
          )}
        </div>
        <Form.Text className="d-block">{t('images.hint')}</Form.Text>
        {/* hidden control so react-bootstrap renders the feedback below */}
        <Form.Control type="text" isInvalid={images.isInvalid} hidden />
        <Form.Control.Feedback type="invalid">
          {images.errorMsg}
        </Form.Control.Feedback>
      </Form.Group>

      <Form.Group controlId="price" className="mb-3">
        <Form.Label>{t('price.label')}</Form.Label>
        <InputGroup hasValidation>
          <InputGroup.Text>¥</InputGroup.Text>
          <Form.Control
            type="number"
            min={0}
            step="0.01"
            inputMode="decimal"
            value={price.value}
            isInvalid={price.isInvalid}
            placeholder={t('price.placeholder')}
            onChange={(e) => onPriceChange(e.currentTarget.value)}
          />
          <Form.Control.Feedback type="invalid">
            {price.errorMsg}
          </Form.Control.Feedback>
        </InputGroup>
      </Form.Group>

      <Form.Group controlId="link" className="mb-3">
        <Form.Label>{t('link.label')}</Form.Label>
        <Form.Control
          type="url"
          value={link.value}
          isInvalid={link.isInvalid}
          placeholder={t('link.placeholder')}
          onChange={(e) => onLinkChange(e.currentTarget.value)}
        />
        <Form.Control.Feedback type="invalid">
          {link.errorMsg}
        </Form.Control.Feedback>
      </Form.Group>
    </>
  );
};

export default PurchaseFields;
