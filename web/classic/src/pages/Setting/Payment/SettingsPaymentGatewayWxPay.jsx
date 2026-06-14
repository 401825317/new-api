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

import React, { useEffect, useRef, useState } from 'react';
import { Banner, Button, Col, Form, Row, Spin } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { Info } from 'lucide-react';
import {
  API,
  removeTrailingSlash,
  showError,
  showSuccess,
  toBoolean,
} from '../../../helpers';

export default function SettingsPaymentGatewayWxPay(props) {
  const { t } = useTranslation();
  const sectionTitle = props.hideSectionTitle ? undefined : t('微信支付设置');
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({
    WxPayEnabled: false,
    WxPayAppID: '',
    WxPayMchID: '',
    WxPayPrivateKey: '',
    WxPayAPIv3Key: '',
    WxPayCertSerial: '',
    WxPayPublicKey: '',
    WxPayPublicKeyID: '',
    WxPayNotifyURL: '',
  });
  const formApiRef = useRef(null);

  useEffect(() => {
    if (props.options && formApiRef.current) {
      const currentInputs = {
        WxPayEnabled: toBoolean(props.options.WxPayEnabled),
        WxPayAppID: props.options.WxPayAppID || '',
        WxPayMchID: props.options.WxPayMchID || '',
        WxPayPrivateKey: props.options.WxPayPrivateKey || '',
        WxPayAPIv3Key: props.options.WxPayAPIv3Key || '',
        WxPayCertSerial: props.options.WxPayCertSerial || '',
        WxPayPublicKey: props.options.WxPayPublicKey || '',
        WxPayPublicKeyID: props.options.WxPayPublicKeyID || '',
        WxPayNotifyURL: props.options.WxPayNotifyURL || '',
      };
      setInputs(currentInputs);
      formApiRef.current.setValues(currentInputs);
    }
  }, [props.options]);

  const handleFormChange = (values) => {
    setInputs(values);
  };

  const submitWxPaySetting = async () => {
    const apiV3Key = (inputs.WxPayAPIv3Key || '').trim();
    if (apiV3Key && apiV3Key.length !== 32) {
      showError(t('APIv3 密钥必须为 32 位'));
      return;
    }

    setLoading(true);
    try {
      const options = [
        {
          key: 'WxPayEnabled',
          value: inputs.WxPayEnabled ? 'true' : 'false',
        },
        { key: 'WxPayAppID', value: (inputs.WxPayAppID || '').trim() },
        { key: 'WxPayMchID', value: (inputs.WxPayMchID || '').trim() },
        {
          key: 'WxPayCertSerial',
          value: (inputs.WxPayCertSerial || '').trim(),
        },
        {
          key: 'WxPayPublicKeyID',
          value: (inputs.WxPayPublicKeyID || '').trim(),
        },
        {
          key: 'WxPayNotifyURL',
          value: removeTrailingSlash((inputs.WxPayNotifyURL || '').trim()),
        },
      ];

      if ((inputs.WxPayPrivateKey || '').trim()) {
        options.push({
          key: 'WxPayPrivateKey',
          value: inputs.WxPayPrivateKey.trim(),
        });
      }
      if (apiV3Key) {
        options.push({ key: 'WxPayAPIv3Key', value: apiV3Key });
      }
      if ((inputs.WxPayPublicKey || '').trim()) {
        options.push({
          key: 'WxPayPublicKey',
          value: inputs.WxPayPublicKey.trim(),
        });
      }

      const results = await Promise.all(
        options.map((opt) =>
          API.put('/api/option/', {
            key: opt.key,
            value: opt.value,
          }),
        ),
      );

      const errorResults = results.filter((res) => !res.data.success);
      if (errorResults.length > 0) {
        errorResults.forEach((res) => showError(res.data.message));
      } else {
        showSuccess(t('更新成功'));
        props.refresh?.();
      }
    } catch (error) {
      showError(t('更新失败'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Spin spinning={loading}>
      <Form
        initValues={inputs}
        onValueChange={handleFormChange}
        getFormApi={(api) => (formApiRef.current = api)}
      >
        <Form.Section text={sectionTitle}>
          <Banner
            type='info'
            icon={<Info size={16} />}
            description={t(
              '直连微信支付 Native 扫码支付。默认回调地址为 <ServerAddress>/api/user/wxpay/notify；如需自定义可填写通知地址。',
            )}
            style={{ marginBottom: 16 }}
            closeIcon={null}
          />
          <Form.Switch field='WxPayEnabled' label={t('启用微信支付')} />

          <Row gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='WxPayAppID'
                label='AppID'
                placeholder='wx...'
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='WxPayMchID'
                label={t('商户号 MchID')}
                placeholder={t('微信支付商户号')}
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='WxPayCertSerial'
                label={t('商户证书序列号')}
                placeholder={t('cert serial')}
              />
            </Col>
          </Row>

          <Row
            gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}
            style={{ marginTop: 16 }}
          >
            <Col xs={24} sm={24} md={12} lg={12} xl={12}>
              <Form.Input
                field='WxPayAPIv3Key'
                label='APIv3 Key'
                placeholder={t(
                  '敏感信息不会发送到前端显示，填写 32 位密钥以更新',
                )}
                type='password'
              />
            </Col>
            <Col xs={24} sm={24} md={12} lg={12} xl={12}>
              <Form.Input
                field='WxPayPublicKeyID'
                label={t('微信支付公钥 ID')}
                placeholder='PUB_KEY_ID_...'
              />
            </Col>
          </Row>

          <Row
            gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}
            style={{ marginTop: 16 }}
          >
            <Col xs={24} sm={24} md={12} lg={12} xl={12}>
              <Form.TextArea
                field='WxPayPrivateKey'
                label={t('商户私钥')}
                placeholder={t(
                  '可粘贴 PEM 或去掉头尾后的私钥正文，留空表示不更新',
                )}
                autosize={{ minRows: 5, maxRows: 10 }}
              />
            </Col>
            <Col xs={24} sm={24} md={12} lg={12} xl={12}>
              <Form.TextArea
                field='WxPayPublicKey'
                label={t('微信支付公钥')}
                placeholder={t(
                  '可粘贴 PEM 或去掉头尾后的公钥正文，留空表示不更新',
                )}
                autosize={{ minRows: 5, maxRows: 10 }}
              />
            </Col>
          </Row>

          <Form.Input
            field='WxPayNotifyURL'
            label={t('通知地址（可选）')}
            placeholder='https://yourdomain.com/api/user/wxpay/notify'
            style={{ marginTop: 16 }}
          />

          <Button onClick={submitWxPaySetting} style={{ marginTop: 16 }}>
            {t('更新微信支付设置')}
          </Button>
        </Form.Section>
      </Form>
    </Spin>
  );
}
