/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useState, useEffect } from 'react';
import { Modal, Button, Typography, Spin } from '@douyinfe/semi-ui';
import { IconExternalOpen, IconCopy } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';

const { Text } = Typography;

const MediaPreviewModal = ({
  isModalOpen,
  setIsModalOpen,
  url,
  previewUrl,
  mediaType,
}) => {
  const { t } = useTranslation();
  const [hasError, setHasError] = useState(false);
  const [isLoading, setIsLoading] = useState(false);

  const effectiveUrl = previewUrl || url;

  useEffect(() => {
    if (isModalOpen) {
      setHasError(false);
      setIsLoading(mediaType === 'video');
    }
  }, [isModalOpen, mediaType]);

  const handleError = () => {
    setHasError(true);
    setIsLoading(false);
  };

  const handleVideoLoaded = () => {
    setIsLoading(false);
  };

  const handleCopyUrl = () => {
    navigator.clipboard.writeText(url || effectiveUrl);
  };

  const handleOpenInNewTab = () => {
    window.open(url || effectiveUrl, '_blank');
  };

  const renderError = () => (
    <div style={{ textAlign: 'center', padding: '40px' }}>
      <Text type='tertiary' style={{ display: 'block', marginBottom: '16px' }}>
        {mediaType === 'video'
          ? t('视频无法在当前浏览器中播放，这可能是由于：')
          : t('图片加载失败')}
      </Text>
      {mediaType === 'video' && (
        <>
          <Text
            type='tertiary'
            style={{ display: 'block', marginBottom: '8px', fontSize: '12px' }}
          >
            {t('• 视频服务商的跨域限制')}
          </Text>
          <Text
            type='tertiary'
            style={{ display: 'block', marginBottom: '8px', fontSize: '12px' }}
          >
            {t('• 需要特定的请求头或认证')}
          </Text>
          <Text
            type='tertiary'
            style={{
              display: 'block',
              marginBottom: '16px',
              fontSize: '12px',
            }}
          >
            {t('• 防盗链保护机制')}
          </Text>
        </>
      )}
      <div style={{ marginTop: '20px' }}>
        <Button
          icon={<IconExternalOpen />}
          onClick={handleOpenInNewTab}
          style={{ marginRight: '8px' }}
        >
          {t('在新标签页中打开')}
        </Button>
        <Button icon={<IconCopy />} onClick={handleCopyUrl}>
          {t('复制链接')}
        </Button>
      </div>
      {url && (
        <div
          style={{
            marginTop: '16px',
            padding: '8px',
            backgroundColor: 'var(--semi-color-fill-0)',
            borderRadius: '4px',
          }}
        >
          <Text
            type='tertiary'
            style={{ fontSize: '10px', wordBreak: 'break-all' }}
          >
            {url}
          </Text>
        </div>
      )}
    </div>
  );

  const renderVideo = () => (
    <div style={{ position: 'relative', height: '100%' }}>
      {isLoading && (
        <div
          style={{
            position: 'absolute',
            top: '50%',
            left: '50%',
            transform: 'translate(-50%, -50%)',
            zIndex: 10,
          }}
        >
          <Spin size='large' />
        </div>
      )}
      <video
        src={effectiveUrl}
        controls
        style={{
          width: '100%',
          height: '100%',
          maxWidth: '100%',
          maxHeight: '100%',
          objectFit: 'contain',
        }}
        onError={handleError}
        onLoadedData={handleVideoLoaded}
        onLoadStart={() => setIsLoading(true)}
      />
    </div>
  );

  const renderImage = () => (
    <div style={{ textAlign: 'center' }}>
      <img
        src={effectiveUrl}
        alt={t('预览')}
        style={{
          width: '100%',
          maxHeight: '70vh',
          objectFit: 'contain',
        }}
        onError={handleError}
      />
    </div>
  );

  const renderContent = () => {
    if (hasError) return renderError();
    if (mediaType === 'video') return renderVideo();
    return renderImage();
  };

  const isVideo = mediaType === 'video';

  return (
    <Modal
      title={isVideo ? t('视频预览') : t('图片预览')}
      visible={isModalOpen}
      onOk={() => setIsModalOpen(false)}
      onCancel={() => setIsModalOpen(false)}
      closable={null}
      footer={null}
      bodyStyle={{
        height: isVideo ? '70vh' : 'auto',
        maxHeight: '80vh',
        overflow: 'auto',
        padding: hasError ? '0' : '16px',
      }}
      width={isVideo ? '90vw' : 800}
      style={isVideo ? { maxWidth: 960 } : undefined}
    >
      {effectiveUrl ? (
        <>
          {renderContent()}
          {url && !hasError && (
            <div style={{ marginTop: 8, textAlign: 'center' }}>
              <a href={url} target='_blank' rel='noopener noreferrer'>
                {t('在新窗口打开')}
              </a>
            </div>
          )}
        </>
      ) : (
        <Text type='tertiary'>{t('无')}</Text>
      )}
    </Modal>
  );
};

export default MediaPreviewModal;
