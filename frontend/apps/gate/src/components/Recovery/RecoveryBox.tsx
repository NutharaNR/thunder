/**
 * Copyright (c) 2025, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/* eslint-disable @typescript-eslint/no-unsafe-assignment */
/* eslint-disable @typescript-eslint/no-explicit-any */
/* eslint-disable @typescript-eslint/no-unsafe-member-access */
/* eslint-disable @typescript-eslint/no-unsafe-call */

import {Recovery, useAsgardeo, type EmbeddedFlowComponent} from '@asgardeo/react';
import {FlowComponentRenderer, AuthCardLayout, useDesign} from '@thunderid/design';
import {useLogger} from '@thunderid/logger/react';
import {Box, Alert, AlertTitle, CircularProgress} from '@wso2/oxygen-ui';
import type {JSX} from 'react';
import {useState} from 'react';
import {useTranslation} from 'react-i18next';
import ROUTES from '../../constants/routes';

export default function RecoveryBox(): JSX.Element {
  const {resolveFlowTemplateLiterals: resolve} = useAsgardeo();
  const {t} = useTranslation();
  const logger = useLogger('RecoveryBox');
  const {isDesignEnabled} = useDesign();
  const [flowError, setFlowError] = useState<string | null>(null);

  return (
    <AuthCardLayout
      variant="RecoveryBox"
      logo={{
        src: {
          light: `${import.meta.env.BASE_URL}/assets/images/logo.svg`,
          dark: `${import.meta.env.BASE_URL}/assets/images/logo-inverted.svg`,
        },
        alt: {light: '', dark: ''},
      }}
      showLogo={!isDesignEnabled}
      logoDisplay={!isDesignEnabled ? {xs: 'flex', md: 'none'} : {display: 'none'}}
    >
      <Recovery
        afterRecoveryUrl={ROUTES.AUTH.SIGN_IN}
        onError={(error: Error) => {
          logger.error('Recovery error:', error);
        }}
        onFlowChange={(response) => {
          setFlowError((response as {failureReason?: string})?.failureReason ?? null);
        }}
      >
        {({
          fieldErrors,
          error,
          touched,
          isLoading,
          components,
          values,
          handleInputChange,
          handleSubmit,
        }: any) => {
          if (isLoading && !components?.length) {
            return (
              <Box sx={{display: 'flex', justifyContent: 'center', p: 3}}>
                <CircularProgress />
              </Box>
            );
          }

          return (
            <>
              {(flowError ?? error) && (
                <Alert severity="error" sx={{mb: 2}}>
                  <AlertTitle>{t('recovery:errors.failed.title', 'Recovery failed')}</AlertTitle>
                  {flowError ?? error?.message ?? t('recovery:errors.failed.description', 'Something went wrong. Please try again.')}
                </Alert>
              )}
              {(components as EmbeddedFlowComponent[])?.length > 0 && (
                <Box sx={{display: 'flex', flexDirection: 'column', gap: 2}}>
                  {(components as EmbeddedFlowComponent[]).map((component, index) => (
                    <FlowComponentRenderer
                      key={component.id ?? index}
                      component={component}
                      index={index}
                      values={values ?? {}}
                      touched={touched}
                      fieldErrors={fieldErrors}
                      isLoading={isLoading}
                      resolve={resolve}
                      onInputChange={handleInputChange}
                      onSubmit={(action, inputs) => {
                        void handleSubmit(action, inputs);
                      }}
                    />
                  ))}
                </Box>
              )}
            </>
          );
        }}
      </Recovery>
    </AuthCardLayout>
  );
}
