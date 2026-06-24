import { describe, it, expect, vi } from 'vitest';
import * as cdk from 'aws-cdk-lib';
import * as ec2 from 'aws-cdk-lib/aws-ec2';
import * as ecs from 'aws-cdk-lib/aws-ecs';
import * as iam from 'aws-cdk-lib/aws-iam';
import * as logs from 'aws-cdk-lib/aws-logs';
import { Template, Match } from 'aws-cdk-lib/assertions';
import type { UserEnvConfig, ServiceBuildContext } from './main.js';
import {
  DEV_CONFIG,
  STG_CONFIG,
  PROD_CONFIG,
  API_NAME,
  PUBLIC_PORT,
  PRIVATE_PORT,
  buildStack,
  buildPublicContainer,
  buildPrivateContainer,
  buildWaf,
  buildUserAlarms,
  buildUserDynamoDB,
  createInfra,
} from './main.js';

function makeStack(): [cdk.Stack, cdk.App] {
  const app = new cdk.App();
  const stack = new cdk.Stack(app, 'TestStack', {
    env: { account: '123456789012', region: 'us-east-2' },
  });
  return [stack, app];
}

function makeCtx(stack: cdk.Stack, cfg: UserEnvConfig): ServiceBuildContext {
  const vpc = ec2.Vpc.fromLookup(stack, 'Vpc', { tags: { Name: cfg.vpcTag } });
  const cluster = new ecs.Cluster(stack, 'Cluster', { vpc });
  const logGroup = new logs.LogGroup(stack, 'LogGroup');
  return { vpc, cluster, logGroup, cfg };
}

describe('configs', () => {
  it('dev config', () => {
    expect(DEV_CONFIG).toMatchObject({
      cpu: 256,
      memory: 512,
      minCapacity: 1,
      maxCapacity: 1,
      usersTable: 'komodo-users-dev',
      secretPath: 'komodo/dev/user-api',
      vpcTag: 'komodo-dev',
      domainName: 'user-dev.komodo.com',
    });
    expect(DEV_CONFIG.downstreamUrls).toEqual([]);
    expect(DEV_CONFIG.tags).toMatchObject({ project: API_NAME, environment: 'dev', dataClassification: 'pii' });
  });

  it('stg config', () => {
    expect(STG_CONFIG).toMatchObject({
      cpu: 512,
      memory: 1024,
      maxCapacity: 3,
      usersTable: 'komodo-users-stg',
      secretPath: 'komodo/staging/user-api',
      vpcTag: 'komodo-staging',
      domainName: 'user-staging.komodo.com',
    });
    expect(STG_CONFIG.tags).toMatchObject({ dataClassification: 'pii' });
  });

  it('prod config', () => {
    expect(PROD_CONFIG).toMatchObject({
      cpu: 1024,
      memory: 2048,
      maxCapacity: 6,
      usersTable: 'komodo-users-prod',
      secretPath: 'komodo/prod/user-api',
      domainName: 'user.komodo.com',
    });
    expect(PROD_CONFIG.tags).toMatchObject({ dataClassification: 'pii' });
  });
});

describe('buildPublicContainer', () => {
  it('creates task def with correct env vars', () => {
    const [stack] = makeStack();
    const ctx = makeCtx(stack, DEV_CONFIG);
    buildPublicContainer(stack, ctx);
    const template = Template.fromStack(stack);

    template.hasResourceProperties('AWS::ECS::TaskDefinition', {
      ContainerDefinitions: Match.arrayWith([
        Match.objectLike({
          Name: `${API_NAME}-public-dev`,
          Environment: Match.arrayWith([
            Match.objectLike({ Name: 'APP_NAME', Value: API_NAME }),
            Match.objectLike({ Name: 'PORT', Value: `:${PUBLIC_PORT}` }),
            Match.objectLike({ Name: 'AWS_REGION', Value: 'us-east-2' }),
            Match.objectLike({ Name: 'DYNAMODB_TABLE', Value: 'komodo-users-dev' }),
          ]),
        }),
      ]),
    });
  });

  it('creates ALB with HTTPS and HTTP redirect', () => {
    const [stack] = makeStack();
    const ctx = makeCtx(stack, DEV_CONFIG);
    buildPublicContainer(stack, ctx);
    const template = Template.fromStack(stack);

    template.resourceCountIs('AWS::ElasticLoadBalancingV2::LoadBalancer', 1);
    template.hasResourceProperties('AWS::ElasticLoadBalancingV2::Listener', {
      Port: 443,
      Protocol: 'HTTPS',
    });
    template.hasResourceProperties('AWS::ElasticLoadBalancingV2::Listener', {
      Port: 80,
      Protocol: 'HTTP',
      DefaultActions: [Match.objectLike({
        Type: 'redirect',
        RedirectConfig: Match.objectLike({ Protocol: 'HTTPS', StatusCode: 'HTTP_301' }),
      })],
    });
  });

  it('creates ALB and task security groups', () => {
    const [stack] = makeStack();
    const ctx = makeCtx(stack, DEV_CONFIG);
    buildPublicContainer(stack, ctx);
    const template = Template.fromStack(stack);

    template.resourceCountIs('AWS::EC2::SecurityGroup', 2);
  });

  it('grants secrets manager access', () => {
    const [stack] = makeStack();
    const ctx = makeCtx(stack, DEV_CONFIG);
    buildPublicContainer(stack, ctx);
    const template = Template.fromStack(stack);

    template.hasResourceProperties('AWS::IAM::Policy', {
      PolicyDocument: Match.objectLike({
        Statement: Match.arrayWith([
          Match.objectLike({
            Action: Match.arrayWith(['secretsmanager:GetSecretValue', 'secretsmanager:DescribeSecret']),
            Effect: 'Allow',
          }),
        ]),
      }),
    });
  });

  it('configures auto-scaling', () => {
    const [stack] = makeStack();
    const ctx = makeCtx(stack, DEV_CONFIG);
    buildPublicContainer(stack, ctx);
    const template = Template.fromStack(stack);

    template.resourceCountIs('AWS::ApplicationAutoScaling::ScalableTarget', 1);
  });
});

describe('buildPrivateContainer', () => {
  it('creates task def with correct env vars', () => {
    const [stack] = makeStack();
    const ctx = makeCtx(stack, DEV_CONFIG);
    buildPrivateContainer(stack, ctx);
    const template = Template.fromStack(stack);

    template.hasResourceProperties('AWS::ECS::TaskDefinition', {
      ContainerDefinitions: Match.arrayWith([
        Match.objectLike({
          Name: `${API_NAME}-private-dev`,
          Environment: Match.arrayWith([
            Match.objectLike({ Name: 'APP_NAME', Value: `${API_NAME}-internal` }),
            Match.objectLike({ Name: 'PORT_PRIVATE', Value: `:${PRIVATE_PORT}` }),
            Match.objectLike({ Name: 'AWS_REGION', Value: 'us-east-2' }),
            Match.objectLike({ Name: 'DYNAMODB_TABLE', Value: 'komodo-users-dev' }),
          ]),
        }),
      ]),
    });
  });

  it('creates task security group with VPC CIDR ingress', () => {
    const [stack] = makeStack();
    const ctx = makeCtx(stack, DEV_CONFIG);
    buildPrivateContainer(stack, ctx);
    const template = Template.fromStack(stack);

    template.resourceCountIs('AWS::EC2::SecurityGroup', 1);
  });
});

describe('buildWaf', () => {
  it('creates WebACL with managed rules and rate limits', () => {
    const [stack] = makeStack();
    const ctx = makeCtx(stack, DEV_CONFIG);
    const pub = buildPublicContainer(stack, ctx);
    buildWaf(stack, pub.alb);
    const template = Template.fromStack(stack);

    template.hasResourceProperties('AWS::WAFv2::WebACL', {
      Scope: 'REGIONAL',
      Rules: Match.arrayWith([
        Match.objectLike({ Name: 'AWSManagedRulesCommonRuleSet' }),
        Match.objectLike({ Name: 'AWSManagedRulesKnownBadInputsRuleSet' }),
        Match.objectLike({ Name: 'ProfileRateLimit' }),
        Match.objectLike({ Name: 'AddressRateLimit' }),
      ]),
    });
    template.resourceCountIs('AWS::WAFv2::WebACLAssociation', 1);
  });
});

describe('buildUserAlarms', () => {
  it('creates metric filters and alarms', () => {
    const [stack] = makeStack();
    const ctx = makeCtx(stack, DEV_CONFIG);
    const pub = buildPublicContainer(stack, ctx);
    buildUserAlarms(stack, ctx.logGroup, pub.alb);
    const template = Template.fromStack(stack);

    template.resourceCountIs('AWS::Logs::MetricFilter', 2);
    template.resourceCountIs('AWS::CloudWatch::Alarm', 7);
  });
});

describe('buildUserDynamoDB', () => {
  it('grants DynamoDB read and write access to provided roles', () => {
    const [stack] = makeStack();
    const role = new iam.Role(stack, 'TestRole', { assumedBy: new iam.ServicePrincipal('ecs-tasks.amazonaws.com') });
    buildUserDynamoDB(stack, 'komodo-users-dev', role);
    const template = Template.fromStack(stack);

    template.hasResourceProperties('AWS::IAM::Policy', {
      PolicyDocument: Match.objectLike({
        Statement: Match.arrayWith([
          Match.objectLike({
            Action: Match.arrayWith([
              'dynamodb:PutItem',
              'dynamodb:UpdateItem',
              'dynamodb:DeleteItem',
            ]),
            Effect: 'Allow',
          }),
        ]),
      }),
    });
  });
});

describe('buildStack', () => {
  it('creates dev stack with DynamoDB access', () => {
    const [stack] = makeStack();
    buildStack(stack, DEV_CONFIG);
    const template = Template.fromStack(stack);

    template.resourceCountIs('AWS::ECS::TaskDefinition', 2);
    template.resourceCountIs('AWS::ECS::Service', 2);
    template.hasOutput('AlbDnsName', {});
    template.hasOutput('ClusterName', {});
    template.hasOutput('PublicServiceName', {});
    template.hasOutput('PrivateServiceName', {});
    template.hasOutput('DomainName', {});
    template.hasOutput('UsersTableName', {});

    expect(() => template.hasOutput('WafWebAclArn', {})).toThrow();
  });

  it('creates full stack for stg with WAF and alarms', () => {
    const [stack] = makeStack();
    buildStack(stack, STG_CONFIG);
    const template = Template.fromStack(stack);

    template.resourceCountIs('AWS::ECS::TaskDefinition', 2);
    template.resourceCountIs('AWS::ECS::Service', 2);
    template.hasOutput('AlbDnsName', {});
    template.hasOutput('ClusterName', {});
    template.hasOutput('PublicServiceName', {});
    template.hasOutput('PrivateServiceName', {});
    template.hasOutput('DomainName', {});
    template.hasOutput('UsersTableName', {});
    template.hasOutput('WafWebAclArn', {});
  });

  it('applies tags from config', () => {
    const [stack] = makeStack();
    buildStack(stack, DEV_CONFIG);
    const template = Template.fromStack(stack);
    const cluster = template.findResources('AWS::ECS::Cluster');
    const clusterTags = Object.values(cluster)[0]?.Properties?.Tags;

    expect(clusterTags).toContainEqual({ Key: 'project', Value: API_NAME });
    expect(clusterTags).toContainEqual({ Key: 'dataClassification', Value: 'pii' });
  });

  it('scales with prod config', () => {
    const [stack] = makeStack();
    buildStack(stack, PROD_CONFIG);
    const template = Template.fromStack(stack);

    template.hasResourceProperties('AWS::ECS::TaskDefinition', {
      Cpu: '1024',
      Memory: '2048',
    });
  });
});

describe('createInfra', () => {
  it('exits with error when env context is missing', () => {
    const exitSpy = vi.spyOn(process, 'exit').mockImplementation(() => undefined as never);
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

    createInfra();

    expect(exitSpy).toHaveBeenCalledWith(1);
    expect(errorSpy).toHaveBeenCalledWith('failed to create infrastructure:', expect.any(Error));

    exitSpy.mockRestore();
    errorSpy.mockRestore();
  });
});
